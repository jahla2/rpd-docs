package store

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
)

type ReconciliationRunItem struct {
    ID string `json:"id"`
    DocumentID string `json:"document_id"`
    Status string `json:"status"`
    AttemptCount int `json:"attempt_count"`
    LastError *string `json:"last_error,omitempty"`
    StartedAt *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ReconciliationRunState struct {
    Run ReconciliationRun `json:"run"`
    Progress int `json:"progress"`
    CancelRequested bool `json:"cancel_requested"`
    ProcessedCount int `json:"processed_count"`
    MatchedCount int `json:"matched_count"`
    SuggestedCount int `json:"suggested_count"`
    ExceptionCount int `json:"exception_count"`
    Config json.RawMessage `json:"config"`
}

type ReconciliationEvent struct {
    ID int64 `json:"id"`
    EventType string `json:"event_type"`
    Status string `json:"status"`
    Progress int `json:"progress"`
    DocumentID *string `json:"document_id,omitempty"`
    Data json.RawMessage `json:"data,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateQueuedReconciliationRun(
    ctx context.Context,
    tenantID,name,targetDatasetID string,
    documentIDs []string,
    config any,
)(ReconciliationRun,error){
    if len(documentIDs)==0 { return ReconciliationRun{},fmt.Errorf("at least one document is required") }

    configJSON,err:=json.Marshal(config)
    if err!=nil { return ReconciliationRun{},err }
    documentJSON,err:=json.Marshal(documentIDs)
    if err!=nil { return ReconciliationRun{},err }

    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return ReconciliationRun{},err }
    defer tx.Rollback(ctx)

    var r ReconciliationRun
    source:=fmt.Sprintf("documents:%d",len(documentIDs))
    err=tx.QueryRow(ctx,`
      INSERT INTO reconciliation_runs
        (tenant_id,name,source_dataset,target_dataset_id,status,rule_set_version,
         document_ids,config_json,progress)
      VALUES ($1,$2,$3,$4,'QUEUED','invoice-erp-v2',$5::jsonb,$6::jsonb,0)
      RETURNING id::text,tenant_id::text,name,source_dataset,target_dataset_id::text,status,
                rule_set_version,started_at,completed_at,created_at
    `,tenantID,name,source,targetDatasetID,string(documentJSON),string(configJSON)).Scan(
      &r.ID,&r.TenantID,&r.Name,&r.SourceDataset,&r.TargetDatasetID,&r.Status,
      &r.RuleSetVersion,&r.StartedAt,&r.CompletedAt,&r.CreatedAt)
    if err!=nil { return ReconciliationRun{},err }

    for _,documentID:=range documentIDs {
        if _,err=tx.Exec(ctx,`
          INSERT INTO reconciliation_run_items
            (tenant_id,reconciliation_run_id,document_id,status)
          VALUES ($1,$2,$3,'QUEUED')
          ON CONFLICT (reconciliation_run_id,document_id) DO NOTHING
        `,tenantID,r.ID,documentID); err!=nil {
            return ReconciliationRun{},err
        }
    }

    if _,err=tx.Exec(ctx,`
      INSERT INTO reconciliation_events
        (tenant_id,reconciliation_run_id,event_type,status,progress,data_json)
      VALUES ($1,$2,'run.queued','QUEUED',0,jsonb_build_object('document_count',$3))
    `,tenantID,r.ID,len(documentIDs)); err!=nil {
        return ReconciliationRun{},err
    }

    if err=tx.Commit(ctx); err!=nil { return ReconciliationRun{},err }
    return r,nil
}

func (s *Store) GetReconciliationRunState(ctx context.Context,tenantID,runID string)(ReconciliationRunState,error){
    var state ReconciliationRunState
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,name,source_dataset,target_dataset_id::text,status,
             rule_set_version,started_at,completed_at,created_at,
             progress,cancel_requested,processed_count,matched_count,suggested_count,exception_count,config_json
      FROM reconciliation_runs
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,runID).Scan(
      &state.Run.ID,&state.Run.TenantID,&state.Run.Name,&state.Run.SourceDataset,&state.Run.TargetDatasetID,
      &state.Run.Status,&state.Run.RuleSetVersion,&state.Run.StartedAt,&state.Run.CompletedAt,&state.Run.CreatedAt,
      &state.Progress,&state.CancelRequested,&state.ProcessedCount,&state.MatchedCount,
      &state.SuggestedCount,&state.ExceptionCount,&state.Config)
    return state,err
}

func (s *Store) StartReconciliationRun(ctx context.Context,tenantID,runID string) error {
    tag,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_runs
      SET status='RUNNING',started_at=COALESCE(started_at,now()),updated_at=now()
      WHERE tenant_id=$1 AND id=$2 AND status IN ('QUEUED','RETRYING')
    `,tenantID,runID)
    if err!=nil { return err }
    if tag.RowsAffected()==0 {
        state,stateErr:=s.GetReconciliationRunState(ctx,tenantID,runID)
        if stateErr!=nil { return stateErr }
        if state.Run.Status=="RUNNING" { return nil }
        return fmt.Errorf("run cannot start from status %s",state.Run.Status)
    }
    return s.AddReconciliationEvent(ctx,tenantID,runID,"run.started","RUNNING",0,nil,nil)
}

func (s *Store) PendingReconciliationItems(ctx context.Context,tenantID,runID string)([]ReconciliationRunItem,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,document_id::text,status,attempt_count,last_error,started_at,completed_at
      FROM reconciliation_run_items
      WHERE tenant_id=$1 AND reconciliation_run_id=$2 AND status IN ('QUEUED','RETRY')
      ORDER BY created_at,id
    `,tenantID,runID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]ReconciliationRunItem,0)
    for rows.Next(){
        var item ReconciliationRunItem
        if err:=rows.Scan(&item.ID,&item.DocumentID,&item.Status,&item.AttemptCount,&item.LastError,&item.StartedAt,&item.CompletedAt); err!=nil {
            return nil,err
        }
        out=append(out,item)
    }
    return out,rows.Err()
}

func (s *Store) MarkReconciliationItemRunning(ctx context.Context,tenantID,runID,documentID string) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_run_items
      SET status='RUNNING',attempt_count=attempt_count+1,started_at=COALESCE(started_at,now()),updated_at=now()
      WHERE tenant_id=$1 AND reconciliation_run_id=$2 AND document_id=$3
    `,tenantID,runID,documentID)
    return err
}

func (s *Store) CompleteReconciliationItem(ctx context.Context,tenantID,runID,documentID,outcome string,processErr error) error {
    status:="EXCEPTION"
    switch outcome {
    case "MATCHED": status="MATCHED"
    case "SUGGESTED": status="SUGGESTED"
    case "EXCEPTION": status="EXCEPTION"
    }
    var errText any
    if processErr!=nil {
        status="FAILED"
        errText=processErr.Error()
    }

    _,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_run_items
      SET status=$1,last_error=$2,completed_at=now(),updated_at=now()
      WHERE tenant_id=$3 AND reconciliation_run_id=$4 AND document_id=$5
    `,status,errText,tenantID,runID,documentID)
    if err!=nil { return err }
    return s.RefreshReconciliationProgress(ctx,tenantID,runID)
}

func (s *Store) RefreshReconciliationProgress(ctx context.Context,tenantID,runID string) error {
    _,err:=s.pool.Exec(ctx,`
      WITH counts AS (
        SELECT
          count(*)::int AS total,
          count(*) FILTER (WHERE status IN ('MATCHED','MATCHED_GROUP','SUGGESTED','EXCEPTION','FAILED','CANCELLED'))::int AS processed,
          count(*) FILTER (WHERE status IN ('MATCHED','MATCHED_GROUP'))::int AS matched,
          count(*) FILTER (WHERE status='SUGGESTED')::int AS suggested,
          count(*) FILTER (WHERE status IN ('EXCEPTION','FAILED'))::int AS exceptions
        FROM reconciliation_run_items
        WHERE tenant_id=$1 AND reconciliation_run_id=$2
      )
      UPDATE reconciliation_runs r
      SET processed_count=counts.processed,
          matched_count=counts.matched,
          suggested_count=counts.suggested,
          exception_count=counts.exceptions,
          progress=CASE WHEN counts.total=0 THEN 0 ELSE floor((counts.processed::numeric/counts.total::numeric)*100)::int END,
          updated_at=now()
      FROM counts
      WHERE r.tenant_id=$1 AND r.id=$2
    `,tenantID,runID)
    return err
}

func (s *Store) RequestReconciliationCancellation(ctx context.Context,tenantID,runID string) error {
    tag,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_runs SET cancel_requested=true,updated_at=now()
      WHERE tenant_id=$1 AND id=$2 AND status IN ('QUEUED','RUNNING','RETRYING')
    `,tenantID,runID)
    if err!=nil { return err }
    if tag.RowsAffected()==0 { return fmt.Errorf("run is not cancellable") }
    return s.AddReconciliationEvent(ctx,tenantID,runID,"run.cancellation_requested","CANCELLING",0,nil,nil)
}

func (s *Store) ReconciliationCancellationRequested(ctx context.Context,tenantID,runID string)(bool,error){
    var requested bool
    err:=s.pool.QueryRow(ctx,`
      SELECT cancel_requested FROM reconciliation_runs WHERE tenant_id=$1 AND id=$2
    `,tenantID,runID).Scan(&requested)
    return requested,err
}

func (s *Store) CancelReconciliationRun(ctx context.Context,tenantID,runID string) error {
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)
    if _,err=tx.Exec(ctx,`
      UPDATE reconciliation_run_items
      SET status='CANCELLED',completed_at=now(),updated_at=now()
      WHERE tenant_id=$1 AND reconciliation_run_id=$2 AND status IN ('QUEUED','RETRY')
    `,tenantID,runID); err!=nil { return err }
    if _,err=tx.Exec(ctx,`
      UPDATE reconciliation_runs
      SET status='CANCELLED',completed_at=now(),progress=100,updated_at=now()
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,runID); err!=nil { return err }
    if err=tx.Commit(ctx); err!=nil { return err }
    return s.AddReconciliationEvent(ctx,tenantID,runID,"run.cancelled","CANCELLED",100,nil,nil)
}

func (s *Store) FailReconciliationRun(ctx context.Context,tenantID,runID string,cause error) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE reconciliation_runs
      SET status='FAILED',failed_at=now(),completed_at=now(),updated_at=now()
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,runID)
    if err!=nil { return err }
    return s.AddReconciliationEvent(ctx,tenantID,runID,"run.failed","FAILED",100,nil,map[string]any{"error":cause.Error()})
}

func (s *Store) CompleteAsyncReconciliationRun(ctx context.Context,tenantID,runID string) error {
    if err:=s.RefreshReconciliationProgress(ctx,tenantID,runID); err!=nil { return err }
    state,err:=s.GetReconciliationRunState(ctx,tenantID,runID)
    if err!=nil { return err }
    status:="COMPLETED"
    if state.ExceptionCount>0 || state.SuggestedCount>0 { status="COMPLETED_WITH_EXCEPTIONS" }
    _,err=s.pool.Exec(ctx,`
      UPDATE reconciliation_runs
      SET status=$1,progress=100,completed_at=now(),updated_at=now()
      WHERE tenant_id=$2 AND id=$3
    `,status,tenantID,runID)
    if err!=nil { return err }
    return s.AddReconciliationEvent(ctx,tenantID,runID,"run.completed",status,100,nil,map[string]any{
        "processed":state.ProcessedCount,
        "matched":state.MatchedCount,
        "suggested":state.SuggestedCount,
        "exceptions":state.ExceptionCount,
    })
}

func (s *Store) AddReconciliationEvent(
    ctx context.Context,tenantID,runID,eventType,status string,progress int,documentID *string,data map[string]any,
) error {
    var dataArg any
    if data!=nil {
        encoded,err:=json.Marshal(data)
        if err!=nil { return err }
        dataArg=string(encoded)
    }
    _,err:=s.pool.Exec(ctx,`
      INSERT INTO reconciliation_events
        (tenant_id,reconciliation_run_id,event_type,status,progress,document_id,data_json)
      VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
    `,tenantID,runID,eventType,status,progress,documentID,dataArg)
    return err
}

func (s *Store) ListReconciliationEventsAfter(
    ctx context.Context,tenantID,runID string,afterID int64,limit int,
)([]ReconciliationEvent,error){
    if limit<=0 || limit>500 { limit=100 }
    rows,err:=s.pool.Query(ctx,`
      SELECT id,event_type,status,progress,document_id::text,data_json,created_at
      FROM reconciliation_events
      WHERE tenant_id=$1 AND reconciliation_run_id=$2 AND id>$3
      ORDER BY id
      LIMIT $4
    `,tenantID,runID,afterID,limit)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]ReconciliationEvent,0)
    for rows.Next(){
        var event ReconciliationEvent
        if err:=rows.Scan(&event.ID,&event.EventType,&event.Status,&event.Progress,&event.DocumentID,&event.Data,&event.CreatedAt); err!=nil {
            return nil,err
        }
        out=append(out,event)
    }
    return out,rows.Err()
}
