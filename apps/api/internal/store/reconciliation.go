package store

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/reconciliation"
)

type ReconciliationRun struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    Name string `json:"name"`
    SourceDataset string `json:"source_dataset"`
    TargetDatasetID string `json:"target_dataset_id"`
    Status string `json:"status"`
    RuleSetVersion string `json:"rule_set_version"`
    StartedAt *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

type ReconciliationMatch struct {
    ID string `json:"id"`
    SourceRecordID string `json:"source_record_id"`
    TargetRecordID string `json:"target_record_id"`
    Status string `json:"status"`
    Score string `json:"score"`
    MatchingRule string `json:"matching_rule"`
    DifferenceAmount *string `json:"difference_amount,omitempty"`
    Evidence json.RawMessage `json:"evidence"`
    ApprovedBy *string `json:"approved_by,omitempty"`
    ApprovedAt *time.Time `json:"approved_at,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

type ReconciliationException struct {
    ID string `json:"id"`
    SourceRecordID string `json:"source_record_id"`
    ExceptionType string `json:"exception_type"`
    Severity string `json:"severity"`
    Message *string `json:"message,omitempty"`
    Evidence json.RawMessage `json:"evidence,omitempty"`
    Status string `json:"status"`
    CreatedAt time.Time `json:"created_at"`
}

type CandidateRecord struct {
    ID string
    ExternalRecordID string
    RecordDate *time.Time
    SupplierName string
    Reference string
    Currency string
    Amount string
}

func (s *Store) CreateReconciliationRun(ctx context.Context,tenantID,name,targetDatasetID string,documentCount int)(ReconciliationRun,error){
    var r ReconciliationRun
    source:=fmt.Sprintf("documents:%d",documentCount)
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO reconciliation_runs
        (tenant_id,name,source_dataset,target_dataset_id,status,rule_set_version,started_at)
      VALUES ($1,$2,$3,$4,'RUNNING','invoice-erp-v1',now())
      RETURNING id::text,tenant_id::text,name,source_dataset,target_dataset_id::text,status,
                rule_set_version,started_at,completed_at,created_at
    `,tenantID,name,source,targetDatasetID).Scan(
      &r.ID,&r.TenantID,&r.Name,&r.SourceDataset,&r.TargetDatasetID,&r.Status,
      &r.RuleSetVersion,&r.StartedAt,&r.CompletedAt,&r.CreatedAt)
    return r,err
}

func (s *Store) CompleteReconciliationRun(ctx context.Context,tenantID,runID,status string) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_runs SET status=$1,completed_at=now()
      WHERE tenant_id=$2 AND id=$3
    `,status,tenantID,runID)
    return err
}

func (s *Store) GetReconciliationRun(ctx context.Context,tenantID,runID string)(ReconciliationRun,error){
    var r ReconciliationRun
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,name,source_dataset,target_dataset_id::text,status,
             rule_set_version,started_at,completed_at,created_at
      FROM reconciliation_runs WHERE tenant_id=$1 AND id=$2
    `,tenantID,runID).Scan(
      &r.ID,&r.TenantID,&r.Name,&r.SourceDataset,&r.TargetDatasetID,&r.Status,
      &r.RuleSetVersion,&r.StartedAt,&r.CompletedAt,&r.CreatedAt)
    return r,err
}

func (s *Store) CandidateERPRecords(
    ctx context.Context,
    tenantID,datasetID,normalizedReference,normalizedSupplier,currency,amount string,
    recordDate *time.Time,
    tolerance string,
    dateWindowDays int,
)([]CandidateRecord,error){
    var dateArg any
    if recordDate!=nil { dateArg=*recordDate }

    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,COALESCE(external_record_id,''),record_date,
             COALESCE(supplier_name,''),COALESCE(reference,''),COALESCE(currency,''),amount::text
      FROM erp_records
      WHERE tenant_id=$1
        AND dataset_id=$2
        AND amount IS NOT NULL
        AND (
          normalized_reference=$3
          OR (
            ($4='' OR normalized_supplier=$4)
            AND ($5='' OR currency=$5)
            AND amount BETWEEN ($6::numeric-$8::numeric) AND ($6::numeric+$8::numeric)
            AND (
              $7::date IS NULL
              OR record_date IS NULL
              OR record_date BETWEEN ($7::date-$9::int) AND ($7::date+$9::int)
            )
          )
        )
      ORDER BY
        CASE WHEN normalized_reference=$3 THEN 0 ELSE 1 END,
        ABS(amount-$6::numeric),
        record_date NULLS LAST
      LIMIT 50
    `,tenantID,datasetID,normalizedReference,normalizedSupplier,currency,amount,dateArg,tolerance,dateWindowDays)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]CandidateRecord,0)
    for rows.Next(){
        var r CandidateRecord
        if err:=rows.Scan(&r.ID,&r.ExternalRecordID,&r.RecordDate,&r.SupplierName,&r.Reference,&r.Currency,&r.Amount); err!=nil {
            return nil,err
        }
        out=append(out,r)
    }
    return out,rows.Err()
}

func (s *Store) SaveMatch(ctx context.Context,tenantID,runID string,result reconciliation.Result) error {
    evidence,err:=json.Marshal(result.Evidence)
    if err!=nil { return err }
    _,err=s.pool.Exec(ctx,`
      INSERT INTO matches
        (tenant_id,reconciliation_run_id,source_record_id,target_record_id,status,score,
         matching_rule,difference_amount,evidence_json)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9)
      ON CONFLICT (tenant_id,reconciliation_run_id,source_record_id,target_record_id)
      DO UPDATE SET
        status=EXCLUDED.status,
        score=EXCLUDED.score,
        matching_rule=EXCLUDED.matching_rule,
        difference_amount=EXCLUDED.difference_amount,
        evidence_json=EXCLUDED.evidence_json
    `,tenantID,runID,result.SourceID,result.TargetID,result.Status,result.Score,
      result.MatchingRule,result.Difference,evidence)
    return err
}

func (s *Store) SaveException(
    ctx context.Context,
    tenantID,runID,sourceID,exceptionType,severity,message string,
    evidence map[string]any,
) error {
    evidenceJSON,err:=json.Marshal(evidence)
    if err!=nil { return err }

    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    _,err=tx.Exec(ctx,`
      INSERT INTO reconciliation_exceptions
        (tenant_id,reconciliation_run_id,source_record_id,exception_type,severity,message,evidence_json,status)
      VALUES ($1,$2,$3,$4,$5,$6,$7,'OPEN')
      ON CONFLICT (tenant_id,reconciliation_run_id,source_record_id,exception_type)
      DO UPDATE SET
        severity=EXCLUDED.severity,
        message=EXCLUDED.message,
        evidence_json=EXCLUDED.evidence_json,
        status='OPEN'
    `,tenantID,runID,sourceID,exceptionType,severity,message,evidenceJSON)
    if err!=nil { return err }

    _,err=tx.Exec(ctx,`
      INSERT INTO review_tasks
        (tenant_id,reconciliation_run_id,source_record_id,task_type,status,priority,reason)
      VALUES ($1,$2,$3,'RECONCILIATION_EXCEPTION','OPEN',100,$4)
      ON CONFLICT DO NOTHING
    `,tenantID,runID,sourceID,message)
    if err!=nil { return err }

    return tx.Commit(ctx)
}

func (s *Store) ListMatches(ctx context.Context,tenantID,runID string)([]ReconciliationMatch,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,source_record_id,target_record_id::text,status,score::text,
             matching_rule,difference_amount::text,evidence_json,approved_by,approved_at,created_at
      FROM matches
      WHERE tenant_id=$1 AND reconciliation_run_id=$2
      ORDER BY score DESC,created_at
    `,tenantID,runID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ReconciliationMatch,0)
    for rows.Next(){
        var m ReconciliationMatch
        if err:=rows.Scan(&m.ID,&m.SourceRecordID,&m.TargetRecordID,&m.Status,&m.Score,
          &m.MatchingRule,&m.DifferenceAmount,&m.Evidence,&m.ApprovedBy,&m.ApprovedAt,&m.CreatedAt); err!=nil {
            return nil,err
        }
        out=append(out,m)
    }
    return out,rows.Err()
}

func (s *Store) ListExceptions(ctx context.Context,tenantID,runID string)([]ReconciliationException,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,source_record_id,exception_type,severity,message,evidence_json,status,created_at
      FROM reconciliation_exceptions
      WHERE tenant_id=$1 AND reconciliation_run_id=$2
      ORDER BY created_at
    `,tenantID,runID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ReconciliationException,0)
    for rows.Next(){
        var e ReconciliationException
        if err:=rows.Scan(&e.ID,&e.SourceRecordID,&e.ExceptionType,&e.Severity,&e.Message,&e.Evidence,&e.Status,&e.CreatedAt); err!=nil {
            return nil,err
        }
        out=append(out,e)
    }
    return out,rows.Err()
}

func (s *Store) ApproveMatch(ctx context.Context,tenantID,runID,matchID,actor string) error {
    tag,err:=s.pool.Exec(ctx,`
      UPDATE matches SET status='APPROVED',approved_by=$1,approved_at=now()
      WHERE tenant_id=$2 AND reconciliation_run_id=$3 AND id=$4
    `,actor,tenantID,runID,matchID)
    if err!=nil { return err }
    if tag.RowsAffected()==0 { return fmt.Errorf("match not found") }
    return nil
}

func (s *Store) RejectMatch(ctx context.Context,tenantID,runID,matchID,actor string) error {
    tag,err:=s.pool.Exec(ctx,`
      UPDATE matches SET status='REJECTED',approved_by=$1,approved_at=now()
      WHERE tenant_id=$2 AND reconciliation_run_id=$3 AND id=$4
    `,actor,tenantID,runID,matchID)
    if err!=nil { return err }
    if tag.RowsAffected()==0 { return fmt.Errorf("match not found") }
    return nil
}
