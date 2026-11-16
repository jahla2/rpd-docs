package store

import (
    "context"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Store struct { pool *pgxpool.Pool }

type CreateDocumentParams struct {
    DocumentID string
    TenantID string
    OriginalName string
    MimeType string
    SHA256 string
    StorageURI string
    JobID string
    RunID string
    OutboxSubject string
    OutboxMessageKey string
    OutboxPayload []byte
}

type Document struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    OriginalFilename string `json:"original_filename"`
    MimeType string `json:"mime_type"`
    SHA256 string `json:"sha256"`
    DocumentType string `json:"document_type"`
    Status string `json:"status"`
    StorageURI string `json:"storage_uri"`
    CreatedAt time.Time `json:"created_at"`
}

type Job struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    DocumentID string `json:"document_id"`
    RunID string `json:"run_id"`
    Status string `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    StartedAt *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type JobStep struct {
    StepName string `json:"step_name"`
    Status string `json:"status"`
    AttemptCount int `json:"attempt_count"`
    StartedAt *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
    LastError *string `json:"last_error,omitempty"`
}

type Artifact struct {
    ID string `json:"id"`
    ArtifactType string `json:"artifact_type"`
    StorageURI string `json:"storage_uri"`
    ContentType *string `json:"content_type,omitempty"`
    SizeBytes *int64 `json:"size_bytes,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
    pool, err := pgxpool.New(ctx, databaseURL); if err != nil { return nil, err }
    if err := pool.Ping(ctx); err != nil { pool.Close(); return nil, err }
    return &Store{pool:pool}, nil
}
func (s *Store) Close() { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) CreateDocumentAndJob(ctx context.Context, p CreateDocumentParams) error {
    tx, err := s.pool.Begin(ctx); if err != nil { return err }
    defer tx.Rollback(ctx)
    _, err = tx.Exec(ctx, `
        INSERT INTO documents
          (id,tenant_id,original_filename,mime_type,sha256,document_type,status,storage_uri)
        VALUES ($1,$2,$3,$4,$5,'unknown','QUEUED',$6)
    `, p.DocumentID,p.TenantID,p.OriginalName,p.MimeType,p.SHA256,p.StorageURI)
    if err != nil { return err }
    _, err = tx.Exec(ctx, `
        INSERT INTO processing_jobs (id,tenant_id,document_id,run_id,status)
        VALUES ($1,$2,$3,$4,'QUEUED')
    `, p.JobID,p.TenantID,p.DocumentID,p.RunID)
    if err != nil { return err }
    _, err = tx.Exec(ctx, `
        INSERT INTO job_events
          (tenant_id,job_id,document_id,event_type,status,progress,data_json)
        VALUES ($1,$2,$3,'document.received','QUEUED',0,'{}'::jsonb)
    `, p.TenantID,p.JobID,p.DocumentID)
    if err != nil { return err }

    _, err = tx.Exec(ctx, `
        INSERT INTO artifacts
          (tenant_id,document_id,run_id,artifact_type,storage_uri,content_type,sha256)
        VALUES ($1,$2,$3,'ORIGINAL',$4,$5,$6)
    `, p.TenantID,p.DocumentID,p.RunID,p.StorageURI,p.MimeType,p.SHA256)
    if err != nil { return err }

    if len(p.OutboxPayload)>0 {
        _,err=tx.Exec(ctx,`
          INSERT INTO outbox_messages
            (tenant_id,subject,message_key,payload_json,status)
          VALUES ($1,$2,$3,$4::jsonb,'PENDING')
          ON CONFLICT (message_key) DO NOTHING
        `,p.TenantID,p.OutboxSubject,p.OutboxMessageKey,string(p.OutboxPayload))
        if err!=nil { return err }
    }
    return tx.Commit(ctx)
}

func (s *Store) GetDocument(ctx context.Context, tenantID,id string) (Document,error) {
    var d Document
    err := s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,original_filename,mime_type,sha256,
             document_type,status,storage_uri,created_at
      FROM documents WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(&d.ID,&d.TenantID,&d.OriginalFilename,&d.MimeType,&d.SHA256,
      &d.DocumentType,&d.Status,&d.StorageURI,&d.CreatedAt)
    return d,err
}

func (s *Store) GetJob(ctx context.Context, tenantID,id string) (Job,error) {
    var j Job
    err := s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,document_id::text,run_id::text,status,
             created_at,started_at,completed_at
      FROM processing_jobs WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(&j.ID,&j.TenantID,&j.DocumentID,&j.RunID,&j.Status,
      &j.CreatedAt,&j.StartedAt,&j.CompletedAt)
    return j,err
}

func (s *Store) ListJobSteps(ctx context.Context,tenantID,jobID string)([]JobStep,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT step_name,status,attempt_count,started_at,completed_at,last_error
      FROM job_steps
      WHERE tenant_id=$1 AND job_id=$2 AND step_name<>'PIPELINE'
      ORDER BY
        CASE step_name
          WHEN 'PREPROCESS' THEN 10
          WHEN 'OCR' THEN 20
          WHEN 'TABLE' THEN 30
          WHEN 'MAPPING' THEN 40
          WHEN 'VALIDATION' THEN 50
          WHEN 'FINALIZE' THEN 60
          ELSE 100
        END,
        started_at NULLS LAST,step_name
    `,tenantID,jobID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]JobStep,0)
    for rows.Next(){
        var step JobStep
        if err:=rows.Scan(&step.StepName,&step.Status,&step.AttemptCount,&step.StartedAt,&step.CompletedAt,&step.LastError); err!=nil {
            return nil,err
        }
        out=append(out,step)
    }
    return out,rows.Err()
}

func (s *Store) LatestCanonicalArtifact(ctx context.Context,tenantID,documentID string)(Artifact,error){
    var a Artifact
    err:=s.pool.QueryRow(ctx,`
      SELECT a.id::text,a.artifact_type,a.storage_uri,a.content_type,a.size_bytes,a.created_at
      FROM artifacts a
      JOIN document_versions v ON v.id=a.document_version_id
      WHERE a.tenant_id=$1 AND a.document_id=$2 AND a.artifact_type='CANONICAL_JSON'
      ORDER BY v.version_number DESC,a.created_at DESC
      LIMIT 1
    `,tenantID,documentID).Scan(&a.ID,&a.ArtifactType,&a.StorageURI,&a.ContentType,&a.SizeBytes,&a.CreatedAt)
    return a,err
}

func (s *Store) ListArtifacts(ctx context.Context,tenantID,documentID string)([]Artifact,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,artifact_type,storage_uri,content_type,size_bytes,created_at
      FROM artifacts
      WHERE tenant_id=$1 AND document_id=$2
      ORDER BY created_at ASC,id ASC
    `,tenantID,documentID)
    if err!=nil { return nil,err }
    defer rows.Close()

    artifacts:=make([]Artifact,0)
    for rows.Next(){
        var a Artifact
        if err:=rows.Scan(&a.ID,&a.ArtifactType,&a.StorageURI,&a.ContentType,&a.SizeBytes,&a.CreatedAt); err!=nil {
            return nil,err
        }
        artifacts=append(artifacts,a)
    }
    return artifacts,rows.Err()
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
