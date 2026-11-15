package store

import (
    "context"
    "encoding/json"
    "time"
)

type ERPImport struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    DatasetID string `json:"dataset_id"`
    OriginalFilename string `json:"original_filename"`
    StorageURI string `json:"storage_uri"`
    SHA256 string `json:"sha256"`
    SizeBytes int64 `json:"size_bytes"`
    Status string `json:"status"`
    Mapping json.RawMessage `json:"mapping"`
    ValidRows int `json:"valid_rows"`
    InsertedRows int `json:"inserted_rows"`
    DuplicateRows int `json:"duplicate_rows"`
    ErrorCount int `json:"error_count"`
    Errors json.RawMessage `json:"errors"`
    ErrorMessage *string `json:"error_message,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

type ERPImportRecord struct {
    TenantID string
    DatasetID string
    OriginalFilename string
    StorageURI string
    SHA256 string
    SizeBytes int64
    Status string
    Mapping any
    ValidRows int
    InsertedRows int
    DuplicateRows int
    Errors any
    ErrorMessage string
}

func (s *Store) RecordERPImport(ctx context.Context,record ERPImportRecord)(ERPImport,error){
    mappingJSON,_:=json.Marshal(record.Mapping)
    errorsJSON,_:=json.Marshal(record.Errors)
    var result ERPImport
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO erp_imports
        (tenant_id,dataset_id,original_filename,storage_uri,sha256,size_bytes,status,
         mapping_json,valid_rows,inserted_rows,duplicate_rows,error_count,errors_json,error_message)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,
              jsonb_array_length($12::jsonb),$12::jsonb,NULLIF($13,''))
      RETURNING id::text,tenant_id::text,dataset_id::text,original_filename,storage_uri,sha256,
                size_bytes,status,mapping_json,valid_rows,inserted_rows,duplicate_rows,error_count,
                errors_json,error_message,created_at
    `,record.TenantID,record.DatasetID,record.OriginalFilename,record.StorageURI,record.SHA256,
      record.SizeBytes,record.Status,string(mappingJSON),record.ValidRows,record.InsertedRows,
      record.DuplicateRows,string(errorsJSON),record.ErrorMessage).Scan(
      &result.ID,&result.TenantID,&result.DatasetID,&result.OriginalFilename,&result.StorageURI,
      &result.SHA256,&result.SizeBytes,&result.Status,&result.Mapping,&result.ValidRows,
      &result.InsertedRows,&result.DuplicateRows,&result.ErrorCount,&result.Errors,
      &result.ErrorMessage,&result.CreatedAt)
    return result,err
}

func (s *Store) ListERPImports(ctx context.Context,tenantID,datasetID string)([]ERPImport,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,tenant_id::text,dataset_id::text,original_filename,storage_uri,sha256,
             size_bytes,status,mapping_json,valid_rows,inserted_rows,duplicate_rows,error_count,
             errors_json,error_message,created_at
      FROM erp_imports
      WHERE tenant_id=$1 AND dataset_id=$2
      ORDER BY created_at DESC
    `,tenantID,datasetID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]ERPImport,0)
    for rows.Next(){
        var item ERPImport
        if err:=rows.Scan(
          &item.ID,&item.TenantID,&item.DatasetID,&item.OriginalFilename,&item.StorageURI,
          &item.SHA256,&item.SizeBytes,&item.Status,&item.Mapping,&item.ValidRows,
          &item.InsertedRows,&item.DuplicateRows,&item.ErrorCount,&item.Errors,
          &item.ErrorMessage,&item.CreatedAt); err!=nil { return nil,err }
        out=append(out,item)
    }
    return out,rows.Err()
}
