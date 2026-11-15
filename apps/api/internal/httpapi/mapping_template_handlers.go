package httpapi

import (
    "encoding/json"
    "net/http"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/mapping"
)

type createMappingTemplateRequest struct {
    Name string `json:"name"`
    DocumentType string `json:"document_type"`
    Config json.RawMessage `json:"config"`
}

type createMappingVersionRequest struct {
    Config json.RawMessage `json:"config"`
}

func (s *Server) createMappingTemplate(w http.ResponseWriter,r *http.Request){
    var req createMappingTemplateRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,2<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE","invalid JSON body"); return
    }
    req.Name=strings.TrimSpace(req.Name)
    req.DocumentType=strings.ToLower(strings.TrimSpace(req.DocumentType))
    if req.Name=="" || req.DocumentType=="" || len(req.Config)==0 {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE","name, document_type and config are required"); return
    }

    config,err:=mapping.ValidateTemplateConfig(req.Config)
    if err!=nil { writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE",err.Error()); return }
    normalized,_:=json.Marshal(config)

    actor:=strings.TrimSpace(r.Header.Get("X-Actor-ID"))
    template,err:=s.store.CreateMappingTemplate(r.Context(),tenantFrom(r,s.defaultTenantID),req.Name,req.DocumentType,actor,normalized)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create mapping template"); return }
    writeJSON(w,http.StatusCreated,template)
}

func (s *Server) listMappingTemplates(w http.ResponseWriter,r *http.Request){
    items,err:=s.store.ListMappingTemplates(
        r.Context(),tenantFrom(r,s.defaultTenantID),
        strings.ToLower(strings.TrimSpace(r.URL.Query().Get("document_type"))),
        strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
    )
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list mapping templates"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) getMappingTemplate(w http.ResponseWriter,r *http.Request){
    item,err:=s.store.GetMappingTemplate(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","mapping template not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read mapping template"); return }
    writeJSON(w,http.StatusOK,item)
}

func (s *Server) createMappingTemplateVersion(w http.ResponseWriter,r *http.Request){
    var req createMappingVersionRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,2<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE","invalid JSON body"); return
    }
    config,err:=mapping.ValidateTemplateConfig(req.Config)
    if err!=nil { writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE",err.Error()); return }
    normalized,_:=json.Marshal(config)

    actor:=strings.TrimSpace(r.Header.Get("X-Actor-ID"))
    item,err:=s.store.CreateMappingTemplateVersion(
        r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),actor,normalized)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","mapping template not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create template version"); return }
    writeJSON(w,http.StatusCreated,item)
}

func (s *Server) activateMappingTemplate(w http.ResponseWriter,r *http.Request){
    item,err:=s.store.ActivateMappingTemplate(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","mapping template not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to activate mapping template"); return }
    writeJSON(w,http.StatusOK,item)
}
