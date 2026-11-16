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
    AbsoluteTolerance string `json:"absolute_tolerance"`
    DateWindowDays int `json:"date_window_days"`
    AutoAcceptComposite bool `json:"auto_accept_composite"`
    FuzzySupplierThreshold float64 `json:"fuzzy_supplier_threshold"`
    MaxGroupSize int `json:"max_group_size"`
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

func (s *Service) ExecuteQueuedRun(ctx context.Context,tenantID,runID string) error {
    state,err:=s.Store.GetReconciliationRunState(ctx,tenantID,runID)
    if err!=nil { return err }

    switch state.Run.Status {
    case "COMPLETED","COMPLETED_WITH_EXCEPTIONS","CANCELLED","FAILED":
        return nil
    }

    var cfg Config
    if err:=json.Unmarshal(state.Config,&cfg); err!=nil { return fmt.Errorf("decode reconciliation config: %w",err) }
    if cfg.AbsoluteTolerance=="" { cfg.AbsoluteTolerance="0.01" }
    if cfg.DateWindowDays<=0 { cfg.DateWindowDays=3 }
    if cfg.FuzzySupplierThreshold<=0 { cfg.FuzzySupplierThreshold=0.72 }
    if cfg.MaxGroupSize<=0 { cfg.MaxGroupSize=4 }

    if err:=s.Store.StartReconciliationRun(ctx,tenantID,runID); err!=nil { return err }
    items,err:=s.Store.PendingReconciliationItems(ctx,tenantID,runID)
    if err!=nil { return err }

    for _,item:=range items {
        cancelled,cancelErr:=s.Store.ReconciliationCancellationRequested(ctx,tenantID,runID)
        if cancelErr!=nil { return cancelErr }
        if cancelled { return s.Store.CancelReconciliationRun(ctx,tenantID,runID) }

        if err:=s.Store.MarkReconciliationItemRunning(ctx,tenantID,runID,item.DocumentID); err!=nil { return err }
        _=s.Store.AddReconciliationEvent(ctx,tenantID,runID,"document.started","RUNNING",state.Progress,&item.DocumentID,nil)

        outcome,processErr:=s.reconcileDocument(ctx,tenantID,runID,state.Run.TargetDatasetID,item.DocumentID,cfg)
        if processErr!=nil {
            _=s.Store.SaveException(ctx,tenantID,runID,item.DocumentID,
                "PROCESSING_ERROR","ERROR",processErr.Error(),map[string]any{"document_id":item.DocumentID})
        }
        if err:=s.Store.CompleteReconciliationItem(ctx,tenantID,runID,item.DocumentID,outcome,processErr); err!=nil { return err }

        updated,updateErr:=s.Store.GetReconciliationRunState(ctx,tenantID,runID)
        if updateErr==nil {
            eventStatus:="document.completed"
            if processErr!=nil { eventStatus="document.failed" }
            _=s.Store.AddReconciliationEvent(ctx,tenantID,runID,eventStatus,"RUNNING",updated.Progress,&item.DocumentID,map[string]any{
                "outcome":outcome,
            })
            state=updated
        }
    }

    cancelled,cancelErr:=s.Store.ReconciliationCancellationRequested(ctx,tenantID,runID)
    if cancelErr!=nil { return cancelErr }
    if cancelled { return s.Store.CancelReconciliationRun(ctx,tenantID,runID) }

    if err:=s.tryManyToOne(ctx,tenantID,runID,state.Run.TargetDatasetID,cfg); err!=nil {
        return err
    }
    if err:=s.Store.RefreshReconciliationProgress(ctx,tenantID,runID); err!=nil { return err }
    return s.Store.CompleteAsyncReconciliationRun(ctx,tenantID,runID)
}

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

    supplierKeys,err:=s.Store.ResolveSupplierKeys(ctx,tenantID,supplier,cfg.FuzzySupplierThreshold)
    if err!=nil { return "",err }

    candidates,err:=s.Store.CandidateERPRecordsAdvanced(
        ctx,tenantID,runID,datasetID,
        reconciliation.NormalizeReference(reference),
        supplier,
        currency,
        amount,
        supplierKeys,
        invoiceDate,
        cfg.AbsoluteTolerance,
        cfg.DateWindowDays,
        cfg.FuzzySupplierThreshold,
    )
    if err!=nil { return "",err }

    if len(candidates)==0 {
        matched,groupErr:=s.tryOneToMany(ctx,tenantID,runID,datasetID,documentID,supplier,currency,amount,
            supplierKeys,invoiceDate,cfg)
        if groupErr!=nil { return "",groupErr }
        if matched { return "MATCHED",nil }

        if err:=s.Store.SaveException(ctx,tenantID,runID,documentID,
            "UNMATCHED","WARNING","no ERP candidate matched indexed or grouped filters",map[string]any{
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
        result,matchErr:=reconciliation.MatchAdvanced(
            reconciliation.Record{
                ID:documentID,Supplier:supplier,Reference:reference,
                Currency:currency,Amount:amount,Date:sourceDate,
            },
            reconciliation.Record{
                ID:candidate.ID,Supplier:candidate.SupplierName,Reference:candidate.Reference,
                Currency:candidate.Currency,Amount:candidate.Amount,Date:targetDate,
            },
            cfg.AbsoluteTolerance,cfg.DateWindowDays,candidate.SupplierSimilarity,
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


func (s *Service) tryOneToMany(
    ctx context.Context,
    tenantID,runID,datasetID,documentID,supplier,currency,amount string,
    supplierKeys []string,
    invoiceDate *time.Time,
    cfg Config,
)(bool,error){
    candidates,err:=s.Store.CandidateERPGroupRecords(
        ctx,tenantID,runID,datasetID,supplier,currency,amount,supplierKeys,invoiceDate,
        cfg.AbsoluteTolerance,cfg.DateWindowDays,cfg.FuzzySupplierThreshold)
    if err!=nil { return false,err }
    if len(candidates)<2 { return false,nil }

    amounts:=make([]reconciliation.AmountEntry,0,len(candidates))
    byID:=make(map[string]store.AdvancedCandidateRecord,len(candidates))
    for _,candidate:=range candidates {
        amounts=append(amounts,reconciliation.AmountEntry{ID:candidate.ID,Amount:candidate.Amount})
        byID[candidate.ID]=candidate
    }

    combination,ok,err:=reconciliation.FindAmountCombination(amounts,amount,cfg.AbsoluteTolerance,cfg.MaxGroupSize)
    if err!=nil || !ok { return false,err }

    targets:=make([]store.MatchGroupMember,0,len(combination.IDs))
    similarities:=make([]float64,0,len(combination.IDs))
    for _,id:=range combination.IDs {
        candidate:=byID[id]
        targets=append(targets,store.MatchGroupMember{ID:id,Amount:candidate.Amount})
        similarities=append(similarities,candidate.SupplierSimilarity)
    }
    minSimilarity:=1.0
    for _,value:=range similarities { if value<minSimilarity { minSimilarity=value } }
    score:=90
    if combination.Difference=="0.0000" { score=95 }
    if minSimilarity>=0.90 { score+=5 }
    if score>100 { score=100 }

    err=s.Store.SaveOneToManyMatchGroup(
        ctx,tenantID,runID,documentID,targets,amount,combination.Total,combination.Difference,score,
        map[string]any{
            "rule":"one_invoice_to_many_erp_records",
            "supplier_similarity_min":minSimilarity,
            "target_record_ids":combination.IDs,
        })
    return err==nil,err
}


type groupedSourceInvoice struct {
    DocumentID string
    Supplier string
    SupplierKey string
    Currency string
    Amount string
    Date *time.Time
}

func (s *Service) loadGroupedSource(ctx context.Context,tenantID,documentID string)(groupedSourceInvoice,error){
    artifact,err:=s.Store.LatestCanonicalArtifact(ctx,tenantID,documentID)
    if err!=nil { return groupedSourceInvoice{},err }
    data,err:=os.ReadFile(artifact.StorageURI)
    if err!=nil { return groupedSourceInvoice{},err }
    var invoice canonical.Invoice
    if err:=json.Unmarshal(data,&invoice); err!=nil { return groupedSourceInvoice{},err }

    result:=groupedSourceInvoice{
        DocumentID:documentID,
        Supplier:strings.TrimSpace(invoice.SupplierName.Value),
        Currency:strings.ToUpper(strings.TrimSpace(invoice.Currency.Value)),
        Amount:strings.TrimSpace(invoice.GrandTotal.Value),
    }
    result.SupplierKey=reconciliation.NormalizeReference(result.Supplier)
    if value:=strings.TrimSpace(invoice.InvoiceDate.Value); value!="" {
        if parsed,parseErr:=time.Parse("2006-01-02",value); parseErr==nil { result.Date=&parsed }
    }
    if result.SupplierKey=="" || result.Amount=="" {
        return groupedSourceInvoice{},fmt.Errorf("document %s lacks supplier or amount for grouped match",documentID)
    }
    return result,nil
}

func (s *Service) tryManyToOne(
    ctx context.Context,tenantID,runID,datasetID string,cfg Config,
) error {
    unmatched,err:=s.Store.ListUnmatchedRunItems(ctx,tenantID,runID)
    if err!=nil { return err }
    if len(unmatched)<2 { return nil }

    groups:=map[string][]groupedSourceInvoice{}
    for _,item:=range unmatched {
        source,loadErr:=s.loadGroupedSource(ctx,tenantID,item.DocumentID)
        if loadErr!=nil { continue }
        key:=source.SupplierKey+"|"+source.Currency
        groups[key]=append(groups[key],source)
    }

    for _,sources:=range groups {
        if len(sources)<2 { continue }
        supplierKeys,resolveErr:=s.Store.ResolveSupplierKeys(ctx,tenantID,sources[0].Supplier,cfg.FuzzySupplierThreshold)
        if resolveErr!=nil { return resolveErr }

        candidates,candidateErr:=s.Store.CandidateERPTargetsForManyToOne(
            ctx,tenantID,runID,datasetID,sources[0].Supplier,sources[0].Currency,
            supplierKeys,sources[0].Date,cfg.DateWindowDays,cfg.FuzzySupplierThreshold)
        if candidateErr!=nil { return candidateErr }

        sourceEntries:=make([]reconciliation.AmountEntry,0,len(sources))
        byID:=map[string]groupedSourceInvoice{}
        for _,source:=range sources {
            sourceEntries=append(sourceEntries,reconciliation.AmountEntry{ID:source.DocumentID,Amount:source.Amount})
            byID[source.DocumentID]=source
        }

        used:=map[string]bool{}
        for _,candidate:=range candidates {
            available:=make([]reconciliation.AmountEntry,0,len(sourceEntries))
            for _,entry:=range sourceEntries {
                if !used[entry.ID] { available=append(available,entry) }
            }
            if len(available)<2 { break }

            combination,ok,comboErr:=reconciliation.FindAmountCombination(
                available,candidate.Amount,cfg.AbsoluteTolerance,cfg.MaxGroupSize)
            if comboErr!=nil { return comboErr }
            if !ok { continue }

            members:=make([]store.MatchGroupMember,0,len(combination.IDs))
            for _,id:=range combination.IDs {
                source:=byID[id]
                members=append(members,store.MatchGroupMember{ID:id,Amount:source.Amount})
            }
            score:=90
            if combination.Difference=="0.0000" { score=95 }
            if candidate.SupplierSimilarity>=0.90 { score+=5 }
            if score>100 { score=100 }

            if err:=s.Store.SaveManyToOneMatchGroup(
                ctx,tenantID,runID,candidate.ID,members,combination.Total,candidate.Amount,
                combination.Difference,score,map[string]any{
                    "rule":"many_invoices_to_one_erp_record",
                    "supplier_similarity":candidate.SupplierSimilarity,
                    "source_document_ids":combination.IDs,
                }); err!=nil { return err }

            for _,id:=range combination.IDs { used[id]=true }
            _=s.Store.AddReconciliationEvent(ctx,tenantID,runID,"group.matched","RUNNING",0,nil,map[string]any{
                "group_type":"MANY_TO_ONE",
                "source_document_ids":combination.IDs,
                "target_record_id":candidate.ID,
            })
        }
    }
    return nil
}
