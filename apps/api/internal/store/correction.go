package store

import (
    "context"
    "encoding/json"
    "fmt"
)

type DocumentVersionInfo struct {
    ID string
    VersionNumber int
    MappingTemplateID *string
    MappingTemplateVersion *int
}

type CorrectionCommit struct {
    VersionID string
    TenantID string
    DocumentID string
    SourceVersionID string
    PipelineVersion string
    MappingTemplateID *string
    MappingTemplateVersion *int
    CorrectionReason string
    CorrectedBy string
    CanonicalPath string
    CanonicalSize int64
    ValidationPath string
    ValidationSize int64
    RequiresReview bool
    ReviewTaskID string
    AuditBefore json.RawMessage
    AuditAfter json.RawMessage
}

func (s *Store) LatestDocumentVersionInfo(ctx context.Context,tenantID,documentID string)(DocumentVersionInfo,error){
    var info DocumentVersionInfo
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,version_number,mapping_template_id::text,mapping_template_version
      FROM document_versions
      WHERE tenant_id=$1 AND document_id=$2
      ORDER BY version_number DESC
      LIMIT 1
    `,tenantID,documentID).Scan(&info.ID,&info.VersionNumber,&info.MappingTemplateID,&info.MappingTemplateVersion)
    return info,err
}

func (s *Store) CommitHumanCorrection(ctx context.Context,commit CorrectionCommit)(int,error){
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return 0,err }
    defer tx.Rollback(ctx)

    var versionNumber int
    err=tx.QueryRow(ctx,`
      INSERT INTO document_versions
        (id,tenant_id,document_id,version_number,pipeline_version,mapping_template_id,
         mapping_template_version,source_version_id,correction_reason,corrected_by)
      SELECT $1,$2,$3,COALESCE(MAX(version_number),0)+1,$4,$5,$6,$7,$8,$9
      FROM document_versions
      WHERE tenant_id=$2 AND document_id=$3
      RETURNING version_number
    `,commit.VersionID,commit.TenantID,commit.DocumentID,commit.PipelineVersion,
      commit.MappingTemplateID,commit.MappingTemplateVersion,commit.SourceVersionID,
      commit.CorrectionReason,commit.CorrectedBy).Scan(&versionNumber)
    if err!=nil { return 0,err }

    for _,artifact:=range []struct{
        kind,path,contentType string
        size int64
    }{
        {"CANONICAL_JSON",commit.CanonicalPath,"application/json",commit.CanonicalSize},
        {"VALIDATION_JSON",commit.ValidationPath,"application/json",commit.ValidationSize},
    } {
        if _,err=tx.Exec(ctx,`
          INSERT INTO artifacts
            (tenant_id,document_id,document_version_id,artifact_type,storage_uri,content_type,size_bytes)
          VALUES ($1,$2,$3,$4,$5,$6,$7)
        `,commit.TenantID,commit.DocumentID,commit.VersionID,artifact.kind,artifact.path,artifact.contentType,artifact.size); err!=nil {
            return 0,err
        }
    }

    status:="READY_FOR_RECONCILIATION"
    if commit.RequiresReview { status="REVIEW_REQUIRED" }
    if _,err=tx.Exec(ctx,`
      UPDATE documents SET status=$1,updated_at=now()
      WHERE tenant_id=$2 AND id=$3
    `,status,commit.TenantID,commit.DocumentID); err!=nil { return 0,err }

    if commit.ReviewTaskID!="" {
        resolution,_:=json.Marshal(map[string]any{
            "decision":"corrected",
            "document_version_id":commit.VersionID,
            "requires_review":commit.RequiresReview,
        })
        if _,err=tx.Exec(ctx,`
          UPDATE review_tasks
          SET status='RESOLVED',document_version_id=$1,resolution_json=$2::jsonb,
              resolved_by=$3,resolved_at=now()
          WHERE tenant_id=$4 AND id=$5 AND status='OPEN'
        `,commit.VersionID,string(resolution),commit.CorrectedBy,commit.TenantID,commit.ReviewTaskID); err!=nil {
            return 0,err
        }
    }

    if _,err=tx.Exec(ctx,`
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,before_json,after_json)
      VALUES ($1,'USER',$2,'DOCUMENT_CORRECTED','document',$3,$4::jsonb,$5::jsonb)
    `,commit.TenantID,commit.CorrectedBy,commit.DocumentID,string(commit.AuditBefore),string(commit.AuditAfter)); err!=nil {
        return 0,err
    }

    if err=tx.Commit(ctx); err!=nil { return 0,err }
    return versionNumber,nil
}

func (s *Store) GetReviewTask(ctx context.Context,tenantID,taskID string)(ReviewTask,error){
    var task ReviewTask
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,document_id::text,reconciliation_run_id::text,source_record_id,
             task_type,status,assigned_to,priority,reason,resolution_json,resolved_by,created_at,resolved_at
      FROM review_tasks
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,taskID).Scan(
      &task.ID,&task.DocumentID,&task.ReconciliationRunID,&task.SourceRecordID,
      &task.TaskType,&task.Status,&task.AssignedTo,&task.Priority,&task.Reason,
      &task.Resolution,&task.ResolvedBy,&task.CreatedAt,&task.ResolvedAt)
    return task,err
}

var _ = fmt.Sprintf
