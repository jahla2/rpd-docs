package store

import (
    "context"
    "encoding/json"
    "time"
)

type UnmatchedRunItem struct {
    DocumentID string
}

func (s *Store) ListUnmatchedRunItems(ctx context.Context,tenantID,runID string)([]UnmatchedRunItem,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT DISTINCT i.document_id::text
      FROM reconciliation_run_items i
      JOIN reconciliation_exceptions e
        ON e.tenant_id=i.tenant_id
       AND e.reconciliation_run_id=i.reconciliation_run_id
       AND e.source_record_id=i.document_id::text
      WHERE i.tenant_id=$1
        AND i.reconciliation_run_id=$2
        AND i.status='EXCEPTION'
        AND e.status='OPEN'
        AND e.exception_type='UNMATCHED'
      ORDER BY i.document_id::text
    `,tenantID,runID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]UnmatchedRunItem,0)
    for rows.Next(){
        var item UnmatchedRunItem
        if err:=rows.Scan(&item.DocumentID); err!=nil { return nil,err }
        out=append(out,item)
    }
    return out,rows.Err()
}

func (s *Store) CandidateERPTargetsForManyToOne(
    ctx context.Context,
    tenantID,runID,datasetID,sourceSupplier,currency string,
    supplierKeys []string,
    recordDate *time.Time,
    dateWindowDays int,
    fuzzyThreshold float64,
)([]AdvancedCandidateRecord,error){
    var dateArg any
    if recordDate!=nil { dateArg=*recordDate }
    if fuzzyThreshold<=0 { fuzzyThreshold=0.72 }

    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,COALESCE(external_record_id,''),record_date,
             COALESCE(supplier_name,''),COALESCE(reference,''),COALESCE(currency,''),amount::text,
             CASE
               WHEN normalized_supplier=ANY($3::text[]) THEN 1.0
               ELSE similarity(COALESCE(supplier_name,''),$7)
             END AS supplier_similarity
      FROM erp_records
      WHERE tenant_id=$1
        AND dataset_id=$2
        AND amount IS NOT NULL
        AND amount > 0
        AND ($4='' OR currency=$4)
        AND (
          $5::date IS NULL
          OR record_date IS NULL
          OR record_date BETWEEN ($5::date-$6::int) AND ($5::date+$6::int)
        )
        AND (
          normalized_supplier=ANY($3::text[])
          OR similarity(COALESCE(supplier_name,''),$7) >= $8
        )
        AND NOT EXISTS (
          SELECT 1 FROM matches m
          WHERE m.tenant_id=$1 AND m.reconciliation_run_id=$9
            AND m.target_record_id=erp_records.id AND m.status<>'REJECTED'
        )
        AND NOT EXISTS (
          SELECT 1
          FROM reconciliation_match_group_members gm
          JOIN reconciliation_match_groups g ON g.id=gm.group_id
          WHERE gm.tenant_id=$1 AND g.reconciliation_run_id=$9
            AND gm.side='TARGET' AND gm.target_record_id=erp_records.id
        )
      ORDER BY
        CASE WHEN normalized_supplier=ANY($3::text[]) THEN 0 ELSE 1 END,
        record_date NULLS LAST,amount DESC
      LIMIT 75
    `,tenantID,datasetID,supplierKeys,currency,dateArg,dateWindowDays,sourceSupplier,fuzzyThreshold,runID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]AdvancedCandidateRecord,0)
    for rows.Next(){
        var item AdvancedCandidateRecord
        if err:=rows.Scan(&item.ID,&item.ExternalRecordID,&item.RecordDate,&item.SupplierName,
          &item.Reference,&item.Currency,&item.Amount,&item.SupplierSimilarity); err!=nil {
            return nil,err
        }
        out=append(out,item)
    }
    return out,rows.Err()
}

func (s *Store) SaveManyToOneMatchGroup(
    ctx context.Context,tenantID,runID,targetRecordID string,sourceDocuments []MatchGroupMember,
    sourceTotal,targetAmount,difference string,score int,evidence map[string]any,
) error {
    evidenceJSON,err:=json.Marshal(evidence)
    if err!=nil { return err }
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    var groupID string
    err=tx.QueryRow(ctx,`
      INSERT INTO reconciliation_match_groups
        (tenant_id,reconciliation_run_id,group_type,status,score,source_total,target_total,difference_amount,evidence_json)
      VALUES ($1,$2,'MANY_TO_ONE','MATCHED_GROUP',$3,$4::numeric,$5::numeric,$6::numeric,$7::jsonb)
      RETURNING id::text
    `,tenantID,runID,score,sourceTotal,targetAmount,difference,string(evidenceJSON)).Scan(&groupID)
    if err!=nil { return err }

    for _,source:=range sourceDocuments {
        if _,err=tx.Exec(ctx,`
          INSERT INTO reconciliation_match_group_members
            (tenant_id,group_id,side,source_document_id,amount)
          VALUES ($1,$2,'SOURCE',$3,$4::numeric)
        `,tenantID,groupID,source.ID,source.Amount); err!=nil { return err }
    }
    if _,err=tx.Exec(ctx,`
      INSERT INTO reconciliation_match_group_members
        (tenant_id,group_id,side,target_record_id,amount)
      VALUES ($1,$2,'TARGET',$3,$4::numeric)
    `,tenantID,groupID,targetRecordID,targetAmount); err!=nil { return err }

    for _,source:=range sourceDocuments {
        if _,err=tx.Exec(ctx,`
          UPDATE reconciliation_run_items
          SET status='MATCHED_GROUP',last_error=NULL,completed_at=now(),updated_at=now()
          WHERE tenant_id=$1 AND reconciliation_run_id=$2 AND document_id=$3
        `,tenantID,runID,source.ID); err!=nil { return err }

        if _,err=tx.Exec(ctx,`
          UPDATE reconciliation_exceptions
          SET status='RESOLVED'
          WHERE tenant_id=$1 AND reconciliation_run_id=$2
            AND source_record_id=$3 AND exception_type='UNMATCHED' AND status='OPEN'
        `,tenantID,runID,source.ID); err!=nil { return err }

        resolution,_:=json.Marshal(map[string]any{
            "decision":"auto_grouped_match",
            "group_id":groupID,
            "target_record_id":targetRecordID,
        })
        if _,err=tx.Exec(ctx,`
          UPDATE review_tasks
          SET status='RESOLVED',resolution_json=$1::jsonb,resolved_by='SYSTEM_GROUP_MATCH',resolved_at=now()
          WHERE tenant_id=$2 AND reconciliation_run_id=$3
            AND source_record_id=$4 AND status='OPEN'
        `,string(resolution),tenantID,runID,source.ID); err!=nil { return err }
    }

    return tx.Commit(ctx)
}
