package store

import (
    "context"
    "encoding/json"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/reconciliation"
)

type AdvancedCandidateRecord struct {
    CandidateRecord
    SupplierSimilarity float64
}

type MatchGroupMember struct {
    ID string
    Amount string
}

func (s *Store) CandidateERPRecordsAdvanced(
    ctx context.Context,
    tenantID,datasetID,normalizedReference,sourceSupplier,currency,amount string,
    supplierKeys []string,
    recordDate *time.Time,
    tolerance string,
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
               WHEN normalized_supplier=ANY($4::text[]) THEN 1.0
               ELSE similarity(COALESCE(supplier_name,''),$10)
             END AS supplier_similarity
      FROM erp_records
      WHERE tenant_id=$1
        AND dataset_id=$2
        AND amount IS NOT NULL
        AND (
          normalized_reference=$3
          OR (
            ($5='' OR currency=$5)
            AND amount BETWEEN ($6::numeric-$8::numeric) AND ($6::numeric+$8::numeric)
            AND (
              $7::date IS NULL
              OR record_date IS NULL
              OR record_date BETWEEN ($7::date-$9::int) AND ($7::date+$9::int)
            )
            AND (
              normalized_supplier=ANY($4::text[])
              OR similarity(COALESCE(supplier_name,''),$10) >= $11
            )
          )
        )
      ORDER BY
        CASE WHEN normalized_reference=$3 THEN 0 ELSE 1 END,
        CASE WHEN normalized_supplier=ANY($4::text[]) THEN 0 ELSE 1 END,
        ABS(amount-$6::numeric),
        record_date NULLS LAST
      LIMIT 75
    `,tenantID,datasetID,normalizedReference,supplierKeys,currency,amount,dateArg,tolerance,
      dateWindowDays,sourceSupplier,fuzzyThreshold)
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

func (s *Store) CandidateERPGroupRecords(
    ctx context.Context,
    tenantID,datasetID,sourceSupplier,currency,targetAmount string,
    supplierKeys []string,
    recordDate *time.Time,
    tolerance string,
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
               ELSE similarity(COALESCE(supplier_name,''),$9)
             END AS supplier_similarity
      FROM erp_records
      WHERE tenant_id=$1
        AND dataset_id=$2
        AND amount IS NOT NULL
        AND amount > 0
        AND amount <= ($5::numeric+$7::numeric)
        AND ($4='' OR currency=$4)
        AND (
          $6::date IS NULL
          OR record_date IS NULL
          OR record_date BETWEEN ($6::date-$8::int) AND ($6::date+$8::int)
        )
        AND (
          normalized_supplier=ANY($3::text[])
          OR similarity(COALESCE(supplier_name,''),$9) >= $10
        )
      ORDER BY
        CASE WHEN normalized_supplier=ANY($3::text[]) THEN 0 ELSE 1 END,
        amount DESC,record_date NULLS LAST
      LIMIT 30
    `,tenantID,datasetID,supplierKeys,currency,targetAmount,dateArg,tolerance,dateWindowDays,
      sourceSupplier,fuzzyThreshold)
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

func (s *Store) SaveOneToManyMatchGroup(
    ctx context.Context,tenantID,runID,sourceDocumentID string,targetRecords []MatchGroupMember,
    sourceAmount,total,difference string,score int,evidence map[string]any,
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
      VALUES ($1,$2,'ONE_TO_MANY','MATCHED_GROUP',$3,$4::numeric,$5::numeric,$6::numeric,$7::jsonb)
      RETURNING id::text
    `,tenantID,runID,score,sourceAmount,total,difference,string(evidenceJSON)).Scan(&groupID)
    if err!=nil { return err }

    if _,err=tx.Exec(ctx,`
      INSERT INTO reconciliation_match_group_members
        (tenant_id,group_id,side,source_document_id,amount)
      VALUES ($1,$2,'SOURCE',$3,$4::numeric)
    `,tenantID,groupID,sourceDocumentID,sourceAmount); err!=nil { return err }

    for _,target:=range targetRecords {
        if _,err=tx.Exec(ctx,`
          INSERT INTO reconciliation_match_group_members
            (tenant_id,group_id,side,target_record_id,amount)
          VALUES ($1,$2,'TARGET',$3,$4::numeric)
        `,tenantID,groupID,target.ID,target.Amount); err!=nil { return err }
    }
    return tx.Commit(ctx)
}

var _ = reconciliation.NormalizeReference
