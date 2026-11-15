package store

import (
    "context"
    "time"
)

type Export struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    ReconciliationRunID *string `json:"reconciliation_run_id,omitempty"`
    Format string `json:"format"`
    Status string `json:"status"`
    StorageURI string `json:"storage_uri"`
    ContentType string `json:"content_type"`
    SizeBytes int64 `json:"size_bytes"`
    CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateExport(
    ctx context.Context,
    id,tenantID,runID,format,storageURI,contentType string,
    sizeBytes int64,
)(Export,error){
    var e Export
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO exports
        (id,tenant_id,reconciliation_run_id,format,status,storage_uri,content_type,size_bytes)
      VALUES ($1,$2,$3,$4,'COMPLETED',$5,$6,$7)
      RETURNING id::text,tenant_id::text,reconciliation_run_id::text,format,status,
                storage_uri,content_type,size_bytes,created_at
    `,id,tenantID,runID,format,storageURI,contentType,sizeBytes).Scan(
      &e.ID,&e.TenantID,&e.ReconciliationRunID,&e.Format,&e.Status,
      &e.StorageURI,&e.ContentType,&e.SizeBytes,&e.CreatedAt)
    return e,err
}

func (s *Store) GetExport(ctx context.Context,tenantID,id string)(Export,error){
    var e Export
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,reconciliation_run_id::text,format,status,
             storage_uri,content_type,size_bytes,created_at
      FROM exports WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(
      &e.ID,&e.TenantID,&e.ReconciliationRunID,&e.Format,&e.Status,
      &e.StorageURI,&e.ContentType,&e.SizeBytes,&e.CreatedAt)
    return e,err
}
