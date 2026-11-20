package store

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
)

type ReviewTask struct {
    ID string `json:"id"`
    DocumentID *string `json:"document_id,omitempty"`
    ReconciliationRunID *string `json:"reconciliation_run_id,omitempty"`
    SourceRecordID *string `json:"source_record_id,omitempty"`
    TaskType string `json:"task_type"`
    Status string `json:"status"`
    AssignedTo *string `json:"assigned_to,omitempty"`
    Priority int `json:"priority"`
    Reason string `json:"reason"`
    Resolution json.RawMessage `json:"resolution,omitempty"`
    ResolvedBy *string `json:"resolved_by,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func (s *Store) ListReviewTasks(ctx context.Context,tenantID,status string,limit int)([]ReviewTask,error){
    if limit<=0 || limit>500 { limit=100 }
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,document_id::text,reconciliation_run_id::text,source_record_id,
             task_type,status,assigned_to,priority,reason,resolution_json,resolved_by,created_at,resolved_at
      FROM review_tasks
      WHERE tenant_id=$1 AND ($2='' OR status=$2)
      ORDER BY
        CASE WHEN status='OPEN' THEN 0 ELSE 1 END,
        priority ASC,
        created_at ASC
      LIMIT $3
    `,tenantID,status,limit)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ReviewTask,0)
    for rows.Next(){
        var task ReviewTask
        if err:=rows.Scan(&task.ID,&task.DocumentID,&task.ReconciliationRunID,&task.SourceRecordID,
          &task.TaskType,&task.Status,&task.AssignedTo,&task.Priority,&task.Reason,
          &task.Resolution,&task.ResolvedBy,&task.CreatedAt,&task.ResolvedAt); err!=nil {
            return nil,err
        }
        out=append(out,task)
    }
    return out,rows.Err()
}

func (s *Store) ResolveReviewTask(
    ctx context.Context,
    tenantID,taskID,actor string,
    resolution map[string]any,
) error {
    resolutionJSON,err:=json.Marshal(resolution)
    if err!=nil { return err }

    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    var beforeJSON []byte
    err=tx.QueryRow(ctx,`
      SELECT jsonb_build_object(
        'status',status,
        'assigned_to',assigned_to,
        'reason',reason,
        'source_record_id',source_record_id
      )::text::bytea
      FROM review_tasks
      WHERE tenant_id=$1 AND id=$2
      FOR UPDATE
    `,tenantID,taskID).Scan(&beforeJSON)
    if err!=nil { return err }

    tag,err:=tx.Exec(ctx,`
      UPDATE review_tasks
      SET status='RESOLVED',resolution_json=$1,resolved_by=$2,resolved_at=now()
      WHERE tenant_id=$3 AND id=$4 AND status<>'RESOLVED'
    `,resolutionJSON,actor,tenantID,taskID)
    if err!=nil { return err }
    if tag.RowsAffected()==0 { return fmt.Errorf("review task is already resolved") }

    afterJSON,_:=json.Marshal(map[string]any{
        "status":"RESOLVED",
        "resolution":resolution,
        "resolved_by":actor,
    })
    _,err=tx.Exec(ctx,`
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,before_json,after_json)
      VALUES ($1,'USER',$2,'REVIEW_TASK_RESOLVED','review_task',$3,$4::jsonb,$5::jsonb)
    `,tenantID,actor,taskID,string(beforeJSON),afterJSON)
    if err!=nil { return err }

    return tx.Commit(ctx)
}
