package httpapi

import (
    "encoding/json"
    "net/http"
    "strconv"
    "strings"
)

type resolveReviewRequest struct {
    Resolution map[string]any `json:"resolution"`
}

func (s *Server) listReviewTasks(w http.ResponseWriter,r *http.Request){
    status:=strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
    limit:=100
    if v,err:=strconv.Atoi(r.URL.Query().Get("limit")); err==nil && v>0 && v<=500 { limit=v }

    items,err:=s.store.ListReviewTasks(r.Context(),tenantFrom(r,s.defaultTenantID),status,limit)
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list review tasks"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) resolveReviewTask(w http.ResponseWriter,r *http.Request){
    var req resolveReviewRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_REVIEW_RESOLUTION","invalid JSON body"); return
    }
    if req.Resolution==nil { req.Resolution=map[string]any{} }

    actor:=actorFrom(r)

    if err:=s.store.ResolveReviewTask(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),actor,req.Resolution); err!=nil {
        writeError(w,http.StatusBadRequest,"REVIEW_RESOLUTION_FAILED",err.Error()); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"RESOLVED"})
}
