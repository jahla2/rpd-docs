package httpapi

import (
    "encoding/json"
    "net/http"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/exporter"
)

type createOutputProfileRequest struct {
    Name string `json:"name"`
    Format string `json:"format"`
    Columns []string `json:"columns"`
    HeaderAliases map[string]string `json:"header_aliases,omitempty"`
}

func (s *Server) createOutputProfile(w http.ResponseWriter,r *http.Request){
    var req createOutputProfileRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_OUTPUT_PROFILE","invalid JSON body"); return
    }
    req.Name=strings.TrimSpace(req.Name)
    req.Format=strings.ToLower(strings.TrimSpace(req.Format))
    if req.Name=="" { writeError(w,http.StatusBadRequest,"INVALID_OUTPUT_PROFILE","name is required"); return }
    if err:=exporter.ValidateProfile(req.Format,req.Columns,req.HeaderAliases); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_OUTPUT_PROFILE",err.Error()); return
    }
    profile,err:=s.store.CreateOutputProfile(
        r.Context(),tenantFrom(r,s.defaultTenantID),req.Name,req.Format,actorFrom(r),req.Columns,req.HeaderAliases)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create output profile"); return }
    writeJSON(w,http.StatusCreated,profile)
}

func (s *Server) listOutputProfiles(w http.ResponseWriter,r *http.Request){
    items,err:=s.store.ListOutputProfiles(r.Context(),tenantFrom(r,s.defaultTenantID))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list output profiles"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) getOutputProfile(w http.ResponseWriter,r *http.Request){
    item,err:=s.store.GetOutputProfile(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","output profile not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read output profile"); return }
    writeJSON(w,http.StatusOK,item)
}
