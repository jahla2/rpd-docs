package store

import (
    "context"

    "github.com/jackc/pgx/v5"
)

func (s *Store) FindDuplicateDocuments(ctx context.Context,tenantID,sha256 string,limit int)([]string,error){
    if limit<=0 || limit>50 { limit=20 }
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text
      FROM documents
      WHERE tenant_id=$1 AND sha256=$2
      ORDER BY created_at DESC
      LIMIT $3
    `,tenantID,sha256,limit)
    if err!=nil { return nil,err }
    defer rows.Close()

    ids:=make([]string,0)
    for rows.Next(){
        var id string
        if err:=rows.Scan(&id); err!=nil { return nil,err }
        ids=append(ids,id)
    }
    return ids,rows.Err()
}

type CreateReprocessJobParams struct {
    TenantID string
    DocumentID string
    JobID string
    RunID string
    OutboxSubject string
    OutboxMessageKey string
    OutboxPayload []byte
}

func (s *Store) CreateReprocessJob(ctx context.Context,p CreateReprocessJobParams) error {
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    result,err:=tx.Exec(ctx,`
      UPDATE documents
      SET status='QUEUED',updated_at=now()
      WHERE tenant_id=$1 AND id=$2
    `,p.TenantID,p.DocumentID)
    if err!=nil { return err }
    if result.RowsAffected()!=1 { return pgx.ErrNoRows }

    if _,err=tx.Exec(ctx,`
      INSERT INTO processing_jobs
        (id,tenant_id,document_id,run_id,status)
      VALUES ($1,$2,$3,$4,'QUEUED')
    `,p.JobID,p.TenantID,p.DocumentID,p.RunID); err!=nil {
        return err
    }

    if _,err=tx.Exec(ctx,`
      INSERT INTO job_events
        (tenant_id,job_id,document_id,event_type,status,progress,data_json)
      VALUES ($1,$2,$3,'document.reprocess_queued','QUEUED',0,'{}'::jsonb)
    `,p.TenantID,p.JobID,p.DocumentID); err!=nil {
        return err
    }

    if len(p.OutboxPayload)>0 {
        if _,err=tx.Exec(ctx,`
          INSERT INTO outbox_messages
            (tenant_id,subject,message_key,payload_json,status)
          VALUES ($1,$2,$3,$4::jsonb,'PENDING')
          ON CONFLICT (message_key) DO NOTHING
        `,p.TenantID,p.OutboxSubject,p.OutboxMessageKey,string(p.OutboxPayload)); err!=nil {
            return err
        }
    }

    return tx.Commit(ctx)
}
