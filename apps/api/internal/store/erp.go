package store

import (
    "context"
    "encoding/json"
    "fmt"
    "strconv"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/erp"
)

type Dataset struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    Name string `json:"name"`
    SourceType string `json:"source_type"`
    Status string `json:"status"`
    CreatedAt time.Time `json:"created_at"`
}

type ERPRecord struct {
    ID string `json:"id"`
    ExternalRecordID *string `json:"external_record_id,omitempty"`
    RecordDate *time.Time `json:"record_date,omitempty"`
    SupplierName *string `json:"supplier_name,omitempty"`
    NormalizedSupplier *string `json:"normalized_supplier,omitempty"`
    Reference *string `json:"reference,omitempty"`
    NormalizedReference *string `json:"normalized_reference,omitempty"`
    Currency *string `json:"currency,omitempty"`
    Amount *string `json:"amount,omitempty"`
    SourceRow *int `json:"source_row,omitempty"`
    DataJSON json.RawMessage `json:"data"`
    CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateDataset(ctx context.Context,tenantID,name,sourceType string)(Dataset,error){
    var d Dataset
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO erp_datasets (tenant_id,name,source_type,status)
      VALUES ($1,$2,$3,'READY')
      RETURNING id::text,tenant_id::text,name,source_type,status,created_at
    `,tenantID,name,sourceType).Scan(&d.ID,&d.TenantID,&d.Name,&d.SourceType,&d.Status,&d.CreatedAt)
    return d,err
}

func (s *Store) GetDataset(ctx context.Context,tenantID,id string)(Dataset,error){
    var d Dataset
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,name,source_type,status,created_at
      FROM erp_datasets WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(&d.ID,&d.TenantID,&d.Name,&d.SourceType,&d.Status,&d.CreatedAt)
    return d,err
}

func (s *Store) ImportERPRecords(ctx context.Context,tenantID,datasetID string,rows []erp.CanonicalRow)(inserted,skipped int,err error){
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return 0,0,err }
    defer tx.Rollback(ctx)

    for _,row:=range rows {
        raw,marshalErr:=json.Marshal(row.Raw)
        if marshalErr!=nil { return inserted,skipped,marshalErr }

        var date any
        if row.RecordDate!=nil { date=*row.RecordDate }

        commandTag,execErr:=tx.Exec(ctx,`
          INSERT INTO erp_records
            (tenant_id,dataset_id,external_record_id,record_date,supplier_name,normalized_supplier,
             reference,normalized_reference,currency,amount,data_json,source_row,row_hash)
          VALUES
            ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,'')::numeric,$11,$12,$13)
          ON CONFLICT (tenant_id,dataset_id,row_hash) WHERE row_hash IS NOT NULL DO NOTHING
        `,tenantID,datasetID,row.ExternalRecordID,date,nullIfEmpty(row.SupplierName),nullIfEmpty(row.NormalizedSupplier),
          nullIfEmpty(row.Reference),nullIfEmpty(row.NormalizedReference),row.Currency,row.Amount,raw,row.SourceRow,row.RowHash)
        if execErr!=nil { return inserted,skipped,execErr }
        if commandTag.RowsAffected()==0 { skipped++ } else { inserted++ }
    }

    if _,err=tx.Exec(ctx,`
      UPDATE erp_datasets SET status='READY' WHERE tenant_id=$1 AND id=$2
    `,tenantID,datasetID); err!=nil { return inserted,skipped,err }

    return inserted,skipped,tx.Commit(ctx)
}

func (s *Store) ListERPRecords(ctx context.Context,tenantID,datasetID string,limit,offset int)([]ERPRecord,error){
    if limit<=0 || limit>500 { limit=100 }
    if offset<0 { offset=0 }

    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,external_record_id,record_date,supplier_name,normalized_supplier,
             reference,normalized_reference,currency,amount::text,source_row,data_json,created_at
      FROM erp_records
      WHERE tenant_id=$1 AND dataset_id=$2
      ORDER BY source_row NULLS LAST,id
      LIMIT $3 OFFSET $4
    `,tenantID,datasetID,limit,offset)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ERPRecord,0)
    for rows.Next(){
        var r ERPRecord
        if err:=rows.Scan(&r.ID,&r.ExternalRecordID,&r.RecordDate,&r.SupplierName,&r.NormalizedSupplier,
          &r.Reference,&r.NormalizedReference,&r.Currency,&r.Amount,&r.SourceRow,&r.DataJSON,&r.CreatedAt); err!=nil {
            return nil,err
        }
        out=append(out,r)
    }
    return out,rows.Err()
}

func ParsePagination(limitValue,offsetValue string)(int,int){
    limit:=100
    offset:=0
    if v,err:=strconv.Atoi(limitValue); err==nil && v>0 && v<=500 { limit=v }
    if v,err:=strconv.Atoi(offsetValue); err==nil && v>=0 { offset=v }
    return limit,offset
}

func nullIfEmpty(v string) any {
    if v=="" { return nil }
    return v
}

func (s *Store) DatasetRecordCount(ctx context.Context,tenantID,datasetID string)(int64,error){
    var count int64
    err:=s.pool.QueryRow(ctx,`
      SELECT count(*) FROM erp_records WHERE tenant_id=$1 AND dataset_id=$2
    `,tenantID,datasetID).Scan(&count)
    return count,err
}

var _ = fmt.Sprintf
