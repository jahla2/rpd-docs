package httpapi

import (
    "encoding/json"
    "net/http"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/reconservice"
)

type reconciliationRequest struct {
    Name string `json:"name"`
    TargetDatasetID string `json:"target_dataset_id"`
    DocumentIDs []string `json:"document_ids"`
    AbsoluteTolerance string `json:"absolute_tolerance,omitempty"`
    DateWindowDays int `json:"date_window_days,omitempty"`
    AutoAcceptComposite bool `json:"auto_accept_composite,omitempty"`
}

func (s *Server) createReconciliationRun(w http.ResponseWriter,r *http.Request){
    var req reconciliationRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,2<<20))
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
    if len(req.DocumentIDs)>500 {
        writeError(w,http.StatusBadRequest,"INVALID_RECONCILIATION_REQUEST","a synchronous MVP run supports at most 500 documents"); return
    }
    if req.AbsoluteTolerance=="" { req.AbsoluteTolerance="0.01" }
    if req.DateWindowDays==0 { req.DateWindowDays=3 }

    service:=reconservice.New(s.store)
    summary,err:=service.Run(r.Context(),tenantFrom(r,s.defaultTenantID),req.Name,req.TargetDatasetID,req.DocumentIDs,reconservice.Config{
        AbsoluteTolerance:req.AbsoluteTolerance,
        DateWindowDays:req.DateWindowDays,
        AutoAcceptComposite:req.AutoAcceptComposite,
    })
    if err!=nil {
        writeError(w,http.StatusBadRequest,"RECONCILIATION_FAILED",err.Error()); return
    }
    writeJSON(w,http.StatusCreated,summary)
}

func (s *Server) getReconciliationRun(w http.ResponseWriter,r *http.Request){
    run,err:=s.store.GetReconciliationRun(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","reconciliation run not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read reconciliation run"); return }

    matches,_:=s.store.ListMatches(r.Context(),tenantFrom(r,s.defaultTenantID),run.ID)
    exceptions,_:=s.store.ListExceptions(r.Context(),tenantFrom(r,s.defaultTenantID),run.ID)
    writeJSON(w,http.StatusOK,map[string]any{
        "run":run,"match_count":len(matches),"exception_count":len(exceptions),
    })
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
    actor:=strings.TrimSpace(r.Header.Get("X-Actor-ID"))
    if actor=="" { actor="api-user" }
    if err:=s.store.ApproveMatch(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),r.PathValue("matchId"),actor); err!=nil {
        writeError(w,http.StatusNotFound,"NOT_FOUND","match not found"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"APPROVED"})
}

func (s *Server) rejectMatch(w http.ResponseWriter,r *http.Request){
    actor:=strings.TrimSpace(r.Header.Get("X-Actor-ID"))
    if actor=="" { actor="api-user" }
    if err:=s.store.RejectMatch(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),r.PathValue("matchId"),actor); err!=nil {
        writeError(w,http.StatusNotFound,"NOT_FOUND","match not found"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"REJECTED"})
}
