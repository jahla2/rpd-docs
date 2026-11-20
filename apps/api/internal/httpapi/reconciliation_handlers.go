package httpapi

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/reconservice"
)

type reconciliationRequest struct {
    Name string `json:"name"`
    TargetDatasetID string `json:"target_dataset_id"`
    DocumentIDs []string `json:"document_ids"`
    AbsoluteTolerance string `json:"absolute_tolerance,omitempty"`
    DateWindowDays int `json:"date_window_days,omitempty"`
    AutoAcceptComposite bool `json:"auto_accept_composite,omitempty"`
    FuzzySupplierThreshold float64 `json:"fuzzy_supplier_threshold,omitempty"`
    MaxGroupSize int `json:"max_group_size,omitempty"`
}

func (s *Server) createReconciliationRun(w http.ResponseWriter,r *http.Request){
    var req reconciliationRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,4<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_RECONCILIATION_REQUEST","invalid JSON body"); return
    }

    req.Name=strings.TrimSpace(req.Name)
    req.TargetDatasetID=strings.TrimSpace(req.TargetDatasetID)
    if req.Name=="" { req.Name="Invoice ERP Reconciliation" }
    if req.TargetDatasetID=="" || len(req.DocumentIDs)==0 {
        writeError(w,http.StatusBadRequest,"INVALID_RECONCILIATION_REQUEST","target_dataset_id and document_ids are required"); return
    }
    if len(req.DocumentIDs)>10000 {
        writeError(w,http.StatusBadRequest,"INVALID_RECONCILIATION_REQUEST","a run supports at most 10000 documents"); return
    }
    if req.AbsoluteTolerance=="" { req.AbsoluteTolerance="0.01" }
    if req.DateWindowDays==0 { req.DateWindowDays=3 }
    if req.FuzzySupplierThreshold==0 { req.FuzzySupplierThreshold=0.72 }
    if req.MaxGroupSize==0 { req.MaxGroupSize=4 }

    if _,err:=s.store.GetDataset(r.Context(),tenantFrom(r,s.defaultTenantID),req.TargetDatasetID); err!=nil {
        if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","target dataset not found") } else {
            writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read target dataset")
        }
        return
    }

    config:=reconservice.Config{
        AbsoluteTolerance:req.AbsoluteTolerance,
        DateWindowDays:req.DateWindowDays,
        AutoAcceptComposite:req.AutoAcceptComposite,
        FuzzySupplierThreshold:req.FuzzySupplierThreshold,
        MaxGroupSize:req.MaxGroupSize,
    }
    tenantID:=tenantFrom(r,s.defaultTenantID)
    run,err:=s.store.CreateQueuedReconciliationRun(r.Context(),tenantID,req.Name,req.TargetDatasetID,req.DocumentIDs,config)
    if err!=nil {
        writeError(w,http.StatusBadRequest,"RECONCILIATION_CREATE_FAILED",err.Error()); return
    }

    message:=queue.ReconciliationJobMessage{
        SchemaVersion:"1.0",TenantID:tenantID,RunID:run.ID,
    }
    if err:=s.queue.PublishReconciliation(message); err!=nil {
        s.logger.Warn("reconciliation publish deferred to outbox","error",err,"run_id",run.ID)
    } else if err:=s.store.MarkOutboxPublishedByKey(r.Context(),"reconciliation:"+run.ID); err!=nil {
        s.logger.Warn("mark reconciliation outbox published failed","error",err,"run_id",run.ID)
    }

    writeJSON(w,http.StatusAccepted,map[string]any{
        "run_id":run.ID,
        "status":"QUEUED",
        "document_count":len(req.DocumentIDs),
        "events_url":fmt.Sprintf("/api/v1/reconciliation-runs/%s/events",run.ID),
    })
}

func (s *Server) getReconciliationRun(w http.ResponseWriter,r *http.Request){
    state,err:=s.store.GetReconciliationRunState(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","reconciliation run not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read reconciliation run"); return }
    writeJSON(w,http.StatusOK,state)
}

func (s *Server) cancelReconciliationRun(w http.ResponseWriter,r *http.Request){
    if err:=s.store.RequestReconciliationCancellation(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id")); err!=nil {
        writeError(w,http.StatusConflict,"RUN_NOT_CANCELLABLE",err.Error()); return
    }
    writeJSON(w,http.StatusAccepted,map[string]any{"status":"CANCELLING"})
}

func (s *Server) streamReconciliationEvents(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    runID:=r.PathValue("id")
    state,err:=s.store.GetReconciliationRunState(r.Context(),tenantID,runID)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","reconciliation run not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read reconciliation run"); return }

    flusher,ok:=w.(http.Flusher)
    if !ok { writeError(w,http.StatusInternalServerError,"SSE_UNSUPPORTED","streaming is unavailable"); return }

    afterID:=int64(0)
    for _,value:=range []string{r.Header.Get("Last-Event-ID"),r.URL.Query().Get("after")} {
        if parsed,parseErr:=strconv.ParseInt(strings.TrimSpace(value),10,64); parseErr==nil && parsed>afterID {
            afterID=parsed
        }
    }

    w.Header().Set("Content-Type","text/event-stream")
    w.Header().Set("Cache-Control","no-cache, no-transform")
    w.Header().Set("Connection","keep-alive")
    w.Header().Set("X-Accel-Buffering","no")
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w,": connected reconciliation=%s\n\n",runID)
    flusher.Flush()

    ticker:=time.NewTicker(750*time.Millisecond)
    heartbeat:=time.NewTicker(15*time.Second)
    defer ticker.Stop(); defer heartbeat.Stop()

    terminal:=isTerminalReconciliationStatus(state.Run.Status)
    send:=func()(int,error){
        events,eventErr:=s.store.ListReconciliationEventsAfter(r.Context(),tenantID,runID,afterID,200)
        if eventErr!=nil { return 0,eventErr }
        for _,event:=range events {
            payload,_:=json.Marshal(event)
            if _,writeErr:=fmt.Fprintf(w,"id: %d\nevent: %s\ndata: %s\n\n",event.ID,event.EventType,payload); writeErr!=nil {
                return 0,writeErr
            }
            afterID=event.ID
            if isTerminalReconciliationStatus(event.Status) { terminal=true }
        }
        if len(events)>0 { flusher.Flush() }
        return len(events),nil
    }
    if _,err:=send(); err!=nil { return }

    for {
        select {
        case <-r.Context().Done(): return
        case <-heartbeat.C:
            if _,err:=fmt.Fprint(w,": heartbeat\n\n"); err!=nil { return }
            flusher.Flush()
        case <-ticker.C:
            count,err:=send()
            if err!=nil { return }
            if count==0 {
                current,currentErr:=s.store.GetReconciliationRunState(r.Context(),tenantID,runID)
                if currentErr==nil && isTerminalReconciliationStatus(current.Run.Status) { terminal=true }
                if terminal {
                    fmt.Fprint(w,": stream-complete\n\n")
                    flusher.Flush()
                    return
                }
            }
        }
    }
}

func isTerminalReconciliationStatus(status string) bool {
    switch status {
    case "COMPLETED","COMPLETED_WITH_EXCEPTIONS","FAILED","CANCELLED":
        return true
    default:
        return false
    }
}

func (s *Server) listReconciliationMatches(w http.ResponseWriter,r *http.Request){
    items,err:=s.store.ListMatches(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list matches"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) listReconciliationExceptions(w http.ResponseWriter,r *http.Request){
    items,err:=s.store.ListExceptions(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list exceptions"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) approveMatch(w http.ResponseWriter,r *http.Request){
    actor:=actorFrom(r)
    if err:=s.store.ApproveMatch(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),r.PathValue("matchId"),actor); err!=nil {
        writeError(w,http.StatusNotFound,"NOT_FOUND","match not found"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"APPROVED"})
}

func (s *Server) rejectMatch(w http.ResponseWriter,r *http.Request){
    actor:=actorFrom(r)
    if err:=s.store.RejectMatch(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),r.PathValue("matchId"),actor); err!=nil {
        writeError(w,http.StatusNotFound,"NOT_FOUND","match not found"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"REJECTED"})
}
