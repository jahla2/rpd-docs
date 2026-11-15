package reconservice

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "sort"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/canonical"
    "github.com/jahla2/rpd-docs/apps/api/internal/reconciliation"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

type Config struct {
    AbsoluteTolerance string
    DateWindowDays int
    AutoAcceptComposite bool
}

type Summary struct {
    RunID string `json:"run_id"`
    Processed int `json:"processed"`
    Matched int `json:"matched"`
    Suggested int `json:"suggested"`
    Exceptions int `json:"exceptions"`
    Status string `json:"status"`
}

type Service struct {
    Store *store.Store
}

func New(st *store.Store)*Service { return &Service{Store:st} }

func (s *Service) Run(
    ctx context.Context,
    tenantID,name,targetDatasetID string,
    documentIDs []string,
    cfg Config,
)(Summary,error){
    if len(documentIDs)==0 { return Summary{},fmt.Errorf("at least one document_id is required") }
    if cfg.AbsoluteTolerance=="" { cfg.AbsoluteTolerance="0.01" }
    if cfg.DateWindowDays<0 || cfg.DateWindowDays>365 { return Summary{},fmt.Errorf("date_window_days must be between 0 and 365") }

    if _,err:=s.Store.GetDataset(ctx,tenantID,targetDatasetID); err!=nil {
        return Summary{},fmt.Errorf("target dataset: %w",err)
    }

    run,err:=s.Store.CreateReconciliationRun(ctx,tenantID,name,targetDatasetID,len(documentIDs))
    if err!=nil { return Summary{},err }

    summary:=Summary{RunID:run.ID,Status:"RUNNING"}

    for _,documentID:=range documentIDs {
        summary.Processed++
        outcome,err:=s.reconcileDocument(ctx,tenantID,run.ID,targetDatasetID,documentID,cfg)
        if err!=nil {
            summary.Exceptions++
            _=s.Store.SaveException(ctx,tenantID,run.ID,documentID,
                "PROCESSING_ERROR","ERROR",err.Error(),map[string]any{"document_id":documentID})
            continue
        }

        switch outcome {
        case "MATCHED":
            summary.Matched++
        case "SUGGESTED":
            summary.Suggested++
            summary.Exceptions++
        default:
            summary.Exceptions++
        }
    }

    finalStatus:="COMPLETED"
    if summary.Exceptions>0 { finalStatus="COMPLETED_WITH_EXCEPTIONS" }
    if err:=s.Store.CompleteReconciliationRun(ctx,tenantID,run.ID,finalStatus); err!=nil {
        return summary,err
    }
    summary.Status=finalStatus
    return summary,nil
}

func (s *Service) reconcileDocument(
    ctx context.Context,
    tenantID,runID,datasetID,documentID string,
    cfg Config,
)(string,error){
    artifact,err:=s.Store.LatestCanonicalArtifact(ctx,tenantID,documentID)
    if err!=nil { return "",fmt.Errorf("canonical document %s: %w",documentID,err) }

    data,err:=os.ReadFile(artifact.StorageURI)
    if err!=nil { return "",fmt.Errorf("read canonical document %s: %w",documentID,err) }

    var invoice canonical.Invoice
    if err:=json.Unmarshal(data,&invoice); err!=nil {
        return "",fmt.Errorf("decode canonical document %s: %w",documentID,err)
    }

    reference:=strings.TrimSpace(invoice.InvoiceNumber.Value)
    supplier:=strings.TrimSpace(invoice.SupplierName.Value)
    currency:=strings.ToUpper(strings.TrimSpace(invoice.Currency.Value))
    amount:=strings.TrimSpace(invoice.GrandTotal.Value)

    if reference=="" || amount=="" {
        message:="canonical invoice is missing invoice_number or grand_total"
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "INVALID_SOURCE","ERROR",message,map[string]any{
                "invoice_number":reference,"grand_total":amount,
            }); err!=nil { return "",err }
        return "EXCEPTION",nil
    }

    var invoiceDate *time.Time
    if value:=strings.TrimSpace(invoice.InvoiceDate.Value); value!="" {
        if parsed,parseErr:=time.Parse("2006-01-02",value); parseErr==nil {
            invoiceDate=&parsed
        }
    }

    candidates,err:=s.Store.CandidateERPRecords(
        ctx,tenantID,datasetID,
        reconciliation.NormalizeReference(reference),
        reconciliation.NormalizeReference(supplier),
        currency,
        amount,
        invoiceDate,
        cfg.AbsoluteTolerance,
        cfg.DateWindowDays,
    )
    if err!=nil { return "",err }

    if len(candidates)==0 {
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "UNMATCHED","WARNING","no ERP candidate matched indexed filters",map[string]any{
                "supplier":supplier,"reference":reference,"currency":currency,
                "amount":amount,"candidate_count":0,
            }); err!=nil { return "",err }
        return "EXCEPTION",nil
    }

    sourceDate:=time.Time{}
    if invoiceDate!=nil { sourceDate=*invoiceDate }

    results:=make([]reconciliation.Result,0,len(candidates))
    for _,candidate:=range candidates {
        targetDate:=time.Time{}
        if candidate.RecordDate!=nil { targetDate=*candidate.RecordDate }
        result,matchErr:=reconciliation.Match(
            reconciliation.Record{
                ID:documentID,Supplier:supplier,Reference:reference,
                Currency:currency,Amount:amount,Date:sourceDate,
            },
            reconciliation.Record{
                ID:candidate.ID,Supplier:candidate.SupplierName,Reference:candidate.Reference,
                Currency:candidate.Currency,Amount:candidate.Amount,Date:targetDate,
            },
            cfg.AbsoluteTolerance,cfg.DateWindowDays,
        )
        if matchErr!=nil { continue }
        results=append(results,result)
    }

    if len(results)==0 {
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "UNMATCHED","WARNING","ERP candidates were found but none could be scored",
            map[string]any{"candidate_count":len(candidates)}); err!=nil { return "",err }
        return "EXCEPTION",nil
    }

    sort.SliceStable(results,func(i,j int)bool{
        if results[i].Score==results[j].Score {
            return results[i].TargetID<results[j].TargetID
        }
        return results[i].Score>results[j].Score
    })

    best:=results[0]
    if best.Status=="UNMATCHED" {
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "UNMATCHED","WARNING","best ERP candidate did not meet match threshold",map[string]any{
                "candidate_count":len(results),"best_score":best.Score,"best_target_id":best.TargetID,
            }); err!=nil { return "",err }
        return "EXCEPTION",nil
    }

    // Equal top scores on multiple ERP records are intentionally not
    // auto-approved. This catches duplicate ERP rows and ambiguous candidates.
    if len(results)>1 && results[1].Score==best.Score && best.Score>=60 {
        candidateIDs:=[]string{best.TargetID,results[1].TargetID}
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "DUPLICATE_OR_AMBIGUOUS","WARNING",
            "multiple ERP records have the same best reconciliation score",
            map[string]any{
                "score":best.Score,"candidate_ids":candidateIDs,
                "reference":reference,"amount":amount,
            }); err!=nil { return "",err }
        return "EXCEPTION",nil
    }

    if best.Status=="SUGGESTED_MATCH" || (best.Status=="MATCHED_COMPOSITE" && !cfg.AutoAcceptComposite) {
        if err:=s.Store.SaveMatch(ctx,tenantID,runID,best); err!=nil { return "",err }
        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "REVIEW_REQUIRED","INFO",
            "candidate match requires reviewer approval",
            map[string]any{
                "target_id":best.TargetID,"score":best.Score,
                "status":best.Status,"rule":best.MatchingRule,
            }); err!=nil { return "",err }
        return "SUGGESTED",nil
    }

    if err:=s.Store.SaveMatch(ctx,tenantID,runID,best); err!=nil { return "",err }
    return "MATCHED",nil
}
