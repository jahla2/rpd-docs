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
    if req.Name=="" || strings.TrimSpace(req.DocumentType)=="" || len(req.Config)==0 {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE","name, document_type and config are required"); return
    }
    documentType,ok:=normalizeDocumentType(req.DocumentType)
    if !ok {
        writeError(w,http.StatusBadRequest,"INVALID_DOCUMENT_TYPE","unsupported document_type"); return
    }
    req.DocumentType=documentType

    config,err:=mapping.ValidateTemplateConfig(req.Config)
    if err!=nil { writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE",err.Error()); return }
    normalized,_:=json.Marshal(config)

    actor:=actorFrom(r)
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

    actor:=actorFrom(r)
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


func (s *Server) previewMappingTemplate(w http.ResponseWriter,r *http.Request){
    var request MappingPreviewRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,2<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&request); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","invalid JSON body"); return
    }
    if len(request.Headers)==0 || len(request.Headers)>200 {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","headers must contain between 1 and 200 values"); return
    }
    for header,values:=range request.Samples {
        if len(values)>100 {
            writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","sample values are limited to 100 per header"); return
        }
        if len(header)>500 {
            writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","sample header is too long"); return
        }
    }

    template,err:=s.store.GetMappingTemplate(
        r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) {
        writeError(w,http.StatusNotFound,"NOT_FOUND","mapping template not found"); return
    }
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read mapping template"); return
    }

    config,err:=mapping.ValidateTemplateConfig(template.Config)
    if err!=nil {
        writeError(w,http.StatusConflict,"INVALID_MAPPING_TEMPLATE","stored mapping template is invalid"); return
    }
    preview:=mapping.PreviewTemplate(config,request.Headers,request.Samples)
    if err:=s.resolveLookupPreview(
        r.Context(),tenantFrom(r,s.defaultTenantID),config,request,&preview); err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to resolve mapping lookup preview"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{
        "template_id":template.ID,
        "template_version":template.Version,
        "template_status":template.Status,
        "document_type":template.DocumentType,
        "detected_source_columns":preview.DetectedSourceColumns,
        "suggestions":preview.Suggestions,
        "sample_transformed_rows":preview.SampleTransformedRows,
        "validation_errors":preview.ValidationErrors,
        "confidence":preview.Confidence,
        "profiles":preview.Profiles,
    })
}
