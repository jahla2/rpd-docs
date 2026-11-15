package store

import (
    "context"
    "encoding/json"
    "time"
)

type ReconciliationMatchGroup struct {
    ID string `json:"id"`
    GroupType string `json:"group_type"`
    Status string `json:"status"`
    Score string `json:"score"`
    SourceTotal string `json:"source_total"`
    TargetTotal string `json:"target_total"`
    DifferenceAmount string `json:"difference_amount"`
    Evidence json.RawMessage `json:"evidence"`
    SourceDocumentIDs []string `json:"source_document_ids"`
    TargetRecordIDs []string `json:"target_record_ids"`
    CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListMatchGroups(ctx context.Context,tenantID,runID string)([]ReconciliationMatchGroup,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT g.id::text,g.group_type,g.status,g.score::text,g.source_total::text,
             g.target_total::text,g.difference_amount::text,g.evidence_json,g.created_at,
             COALESCE(array_agg(m.source_document_id::text ORDER BY m.created_at)
               FILTER (WHERE m.side='SOURCE'),'{}'::text[]),
             COALESCE(array_agg(m.target_record_id::text ORDER BY m.created_at)
               FILTER (WHERE m.side='TARGET'),'{}'::text[])
      FROM reconciliation_match_groups g
      LEFT JOIN reconciliation_match_group_members m
        ON m.group_id=g.id AND m.tenant_id=g.tenant_id
      WHERE g.tenant_id=$1 AND g.reconciliation_run_id=$2
      GROUP BY g.id
      ORDER BY g.created_at,g.id
    `,tenantID,runID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ReconciliationMatchGroup,0)
    for rows.Next(){
        var group ReconciliationMatchGroup
        if err:=rows.Scan(&group.ID,&group.GroupType,&group.Status,&group.Score,
          &group.SourceTotal,&group.TargetTotal,&group.DifferenceAmount,&group.Evidence,&group.CreatedAt,
          &group.SourceDocumentIDs,&group.TargetRecordIDs); err!=nil { return nil,err }
        out=append(out,group)
    }
    return out,rows.Err()
}
