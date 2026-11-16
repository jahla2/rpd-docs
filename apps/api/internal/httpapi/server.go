package httpapi

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "log/slog"
    "mime/multipart"
    "net/http"
    "path/filepath"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/auth"
    "github.com/jahla2/rpd-docs/apps/api/internal/mapping"
    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/storage"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

const maxUploadBytes = 50 << 20

type Server struct {
    store *store.Store
    storage *storage.Manager
    queue *queue.Publisher
    defaultTenantID string
    logger *slog.Logger
    invoiceMappings *mapping.Registry
    apiKeyPepper string
}

type MappingPreviewRequest struct {
    Headers []string `json:"headers"`
    Samples map[string][]string `json:"samples,omitempty"`
}

func New(st *store.Store, fs *storage.Manager, q *queue.Publisher, defaultTenantID,apiKeyPepper string, logger *slog.Logger) http.Handler {
    s := &Server{
        store:st,storage:fs,queue:q,defaultTenantID:defaultTenantID,logger:logger,
        invoiceMappings:mapping.DefaultInvoiceRegistry(),apiKeyPepper:apiKeyPepper,
    }
    mux := http.NewServeMux()
    mux.HandleFunc("GET /health/live", s.live)
    mux.HandleFunc("GET /health/ready", s.ready)
    mux.HandleFunc("POST /api/v1/documents", s.upload)
    mux.HandleFunc("GET /api/v1/documents/{id}", s.getDocument)
    mux.HandleFunc("GET /api/v1/documents/{id}/result", s.getDocumentResult)
    mux.HandleFunc("GET /api/v1/documents/{id}/artifacts", s.getDocumentArtifacts)
    mux.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
    mux.HandleFunc("GET /api/v1/jobs/{id}/steps", s.getJobSteps)
    mux.HandleFunc("GET /api/v1/jobs/{id}/pages", s.listJobPages)
    mux.HandleFunc("GET /api/v1/jobs/{id}/events", s.streamJobEvents)
    mux.HandleFunc("POST /api/v1/mapping-templates/preview", s.previewMapping)
    mux.HandleFunc("GET /api/v1/mapping-templates", s.listMappingTemplates)
    mux.HandleFunc("POST /api/v1/mapping-templates", s.createMappingTemplate)
    mux.HandleFunc("GET /api/v1/mapping-templates/{id}", s.getMappingTemplate)
    mux.HandleFunc("POST /api/v1/mapping-templates/{id}/versions", s.createMappingTemplateVersion)
    mux.HandleFunc("POST /api/v1/mapping-templates/{id}/activate", s.activateMappingTemplate)
    mux.HandleFunc("POST /api/v1/datasets", s.createDataset)
    mux.HandleFunc("GET /api/v1/datasets/{id}", s.getDataset)
    mux.HandleFunc("POST /api/v1/datasets/{id}/import", s.importDataset)
    mux.HandleFunc("GET /api/v1/datasets/{id}/records", s.listDatasetRecords)
    mux.HandleFunc("GET /api/v1/datasets/{id}/imports", s.listDatasetImports)
    mux.HandleFunc("POST /api/v1/reconciliation-runs", s.createReconciliationRun)
    mux.HandleFunc("GET /api/v1/reconciliation-runs/{id}", s.getReconciliationRun)
    mux.HandleFunc("POST /api/v1/reconciliation-runs/{id}/cancel", s.cancelReconciliationRun)
    mux.HandleFunc("GET /api/v1/reconciliation-runs/{id}/events", s.streamReconciliationEvents)
    mux.HandleFunc("GET /api/v1/reconciliation-runs/{id}/matches", s.listReconciliationMatches)
    mux.HandleFunc("GET /api/v1/reconciliation-runs/{id}/exceptions", s.listReconciliationExceptions)
    mux.HandleFunc("POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/approve", s.approveMatch)
    mux.HandleFunc("POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/reject", s.rejectMatch)
    mux.HandleFunc("POST /api/v1/reconciliation-runs/{id}/export", s.exportReconciliationRun)
    mux.HandleFunc("GET /api/v1/review-tasks", s.listReviewTasks)
    mux.HandleFunc("GET /api/v1/review-tasks/{id}", s.getReviewTask)
    mux.HandleFunc("POST /api/v1/review-tasks/{id}/resolve", s.resolveReviewTask)
    mux.HandleFunc("POST /api/v1/documents/{id}/corrections", s.correctDocument)
    mux.HandleFunc("GET /api/v1/exports/{id}", s.getExport)
    mux.HandleFunc("GET /api/v1/exports/{id}/download", s.downloadExport)
    mux.HandleFunc("GET /api/v1/suppliers", s.searchSuppliers)
    mux.HandleFunc("POST /api/v1/suppliers", s.createSupplier)
    mux.HandleFunc("POST /api/v1/suppliers/{id}/aliases", s.addSupplierAlias)
    mux.HandleFunc("GET /api/v1/admin/api-keys", s.listAPIKeys)
    mux.HandleFunc("POST /api/v1/admin/api-keys", s.createAPIKey)
    mux.HandleFunc("POST /api/v1/admin/api-keys/{id}/revoke", s.revokeAPIKey)
    mux.HandleFunc("GET /api/v1/artifacts/{id}/access", s.getArtifactAccess)
    mux.HandleFunc("GET /api/v1/artifacts/{id}/content", s.getArtifactContent)
    mux.HandleFunc("GET /api/v1/output-profiles", s.listOutputProfiles)
    mux.HandleFunc("POST /api/v1/output-profiles", s.createOutputProfile)
    mux.HandleFunc("GET /api/v1/output-profiles/{id}", s.getOutputProfile)
    return requestLog(logger,mux)
}

func (s *Server) live(w http.ResponseWriter,_ *http.Request) {
    writeJSON(w,http.StatusOK,map[string]any{"status":"ok","service":"rpd-api"})
}

func (s *Server) ready(w http.ResponseWriter,r *http.Request) {
    checks := map[string]bool{
        "postgres": s.store.Ping(r.Context()) == nil,
        "storage": s.storage.Ready() == nil,
        "nats": s.queue.Ready(),
    }
    ok := checks["postgres"] && checks["storage"] && checks["nats"]
    status := http.StatusOK; if !ok { status = http.StatusServiceUnavailable }
    writeJSON(w,status,map[string]any{"ready":ok,"checks":checks})
}

func (s *Server) upload(w http.ResponseWriter,r *http.Request) {
    r.Body = http.MaxBytesReader(w,r.Body,maxUploadBytes)
    if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
        writeError(w,http.StatusBadRequest,"INVALID_FILE","invalid multipart upload"); return
    }
    file,header,err := r.FormFile("file")
    if err != nil { writeError(w,http.StatusBadRequest,"INVALID_FILE","multipart field 'file' is required"); return }
    defer file.Close()
    mimeType := header.Header.Get("Content-Type")
    if !allowedUpload(header,mimeType) {
        writeError(w,http.StatusUnsupportedMediaType,"UNSUPPORTED_FORMAT","unsupported file type"); return
    }
    tenantID := tenantFrom(r,s.defaultTenantID)

    options:=map[string]any{}
    if templateID:=strings.TrimSpace(r.FormValue("mapping_template_id")); templateID!="" {
        template,templateErr:=s.store.GetMappingTemplate(r.Context(),tenantID,templateID)
        if store.IsNotFound(templateErr) {
            writeError(w,http.StatusBadRequest,"INVALID_MAPPING_TEMPLATE","mapping template not found"); return
        }
        if templateErr!=nil {
            writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read mapping template"); return
        }
        if template.Status!="ACTIVE" {
            writeError(w,http.StatusConflict,"MAPPING_TEMPLATE_NOT_ACTIVE","selected mapping template must be ACTIVE"); return
        }
        var config any
        if err:=json.Unmarshal(template.Config,&config); err!=nil {
            writeError(w,http.StatusInternalServerError,"INVALID_MAPPING_TEMPLATE","stored mapping template is invalid"); return
        }
        options["mapping_template_id"]=template.ID
        options["mapping_template_version"]=template.Version
        options["mapping_config"]=config
    }

    documentID,jobID,runID := newUUID(),newUUID(),newUUID()
    saved,err := s.storage.SaveOriginal(tenantID,documentID,header.Filename,file)
    if err != nil {
        s.logger.Error("store original","error",err,"document_id",documentID)
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to store original"); return
    }

    if saved.StorageURI!=saved.Path { options["source_uri"]=saved.StorageURI }
    msg := queue.JobMessage{
        SchemaVersion:"1.0",JobID:jobID,TenantID:tenantID,DocumentID:documentID,
        RunID:runID,Step:"PREPROCESS",InputArtifact:saved.Path,Options:options,
    }
    payload,err:=json.Marshal(msg)
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"QUEUE_FAILED","failed to serialize processing job"); return
    }
    messageKey:=jobID+":"+runID+":PREPROCESS"

    if err := s.store.CreateDocumentAndJob(r.Context(),store.CreateDocumentParams{
        DocumentID:documentID,TenantID:tenantID,OriginalName:filepath.Base(header.Filename),
        MimeType:mimeType,SHA256:saved.SHA256,StorageURI:saved.StorageURI,JobID:jobID,RunID:runID,
        OutboxSubject:"document.preprocess",OutboxMessageKey:messageKey,OutboxPayload:payload,
    }); err != nil {
        s.logger.Error("create document","error",err,"document_id",documentID)
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create document"); return
    }

    // Best-effort immediate delivery keeps normal uploads fast. The transactional
    // outbox remains authoritative and retries delivery if NATS is unavailable.
    if err := s.queue.PublishRaw("document.preprocess",payload,messageKey); err != nil {
        s.logger.Warn("publish preprocess deferred to outbox","error",err,"document_id",documentID,"job_id",jobID)
    } else if err:=s.store.MarkOutboxPublishedByKey(r.Context(),messageKey); err!=nil {
        s.logger.Warn("mark outbox published failed","error",err,"document_id",documentID,"job_id",jobID)
    }

    writeJSON(w,http.StatusAccepted,map[string]any{
        "document_id":documentID,"job_id":jobID,"run_id":runID,"status":"QUEUED","sha256":saved.SHA256,
    })
}

func (s *Server) getDocument(w http.ResponseWriter,r *http.Request) {
    d,err := s.store.GetDocument(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if store.IsNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","document not found"); return }
    if err != nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read document"); return }
    writeJSON(w,http.StatusOK,d)
}

func (s *Server) getDocumentResult(w http.ResponseWriter,r *http.Request) {
    tenantID:=tenantFrom(r,s.defaultTenantID)
    artifact,err:=s.store.LatestCanonicalArtifact(r.Context(),tenantID,r.PathValue("id"))
    if store.IsNotFound(err) {
        writeError(w,http.StatusNotFound,"RESULT_NOT_READY","canonical result is not available"); return
    }
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to locate canonical result"); return
    }

    data,err:=s.storage.Read(artifact.StorageURI)
    if err!=nil {
        s.logger.Error("read canonical result","error",err,"artifact_id",artifact.ID)
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","canonical artifact could not be read"); return
    }
    w.Header().Set("Content-Type","application/json")
    w.Header().Set("X-Artifact-ID",artifact.ID)
    w.WriteHeader(http.StatusOK)
    _,_=w.Write(data)
}

func (s *Server) getDocumentArtifacts(w http.ResponseWriter,r *http.Request) {
    artifacts,err:=s.store.ListArtifacts(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list artifacts"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"items":artifacts})
}

func (s *Server) getJob(w http.ResponseWriter,r *http.Request) {
    j,err := s.store.GetJob(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if store.IsNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","job not found"); return }
    if err != nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read job"); return }
    writeJSON(w,http.StatusOK,j)
}

func (s *Server) getJobSteps(w http.ResponseWriter,r *http.Request) {
    tenantID:=tenantFrom(r,s.defaultTenantID)
    steps,err:=s.store.ListJobSteps(r.Context(),tenantID,r.PathValue("id"))
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read job steps"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"items":steps})
}

func (s *Server) previewMapping(w http.ResponseWriter,r *http.Request) {
    var request MappingPreviewRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&request); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","invalid JSON body"); return
    }
    if len(request.Headers)==0 || len(request.Headers)>200 {
        writeError(w,http.StatusBadRequest,"INVALID_MAPPING_REQUEST","headers must contain between 1 and 200 values"); return
    }

    suggestions:=s.invoiceMappings.Suggest(request.Headers)
    inferred:=make(map[string]string,len(request.Headers))
    for _,header:=range request.Headers {
        inferred[header]=mapping.InferType(request.Samples[header])
    }

    writeJSON(w,http.StatusOK,map[string]any{
        "document_type":"invoice",
        "suggestions":suggestions,
        "inferred_types":inferred,
        "profiles":mapping.ProfileColumns(request.Headers,request.Samples),
    })
}

func tenantFrom(r *http.Request,fallback string) string {
    if principal,ok:=auth.PrincipalFrom(r.Context()); ok { return principal.TenantID }
    if v := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); v != "" { return v }
    return fallback
}

func actorFrom(r *http.Request) string {
    if principal,ok:=auth.PrincipalFrom(r.Context()); ok { return principal.ActorID }
    if v:=strings.TrimSpace(r.Header.Get("X-Actor-ID")); v!="" { return v }
    return "api-user"
}
func allowedUpload(h *multipart.FileHeader,mimeType string) bool {
    switch strings.ToLower(filepath.Ext(filepath.Base(h.Filename))) {
    case ".pdf",".png",".jpg",".jpeg",".tif",".tiff": return true
    }
    return strings.HasPrefix(mimeType,"image/")
}
func newUUID() string {
    var b [16]byte
    if _,err := rand.Read(b[:]); err != nil { panic(err) }
    b[6]=(b[6]&0x0f)|0x40; b[8]=(b[8]&0x3f)|0x80
    s:=hex.EncodeToString(b[:])
    return fmt.Sprintf("%s-%s-%s-%s-%s",s[0:8],s[8:12],s[12:16],s[16:20],s[20:32])
}
func writeJSON(w http.ResponseWriter,status int,v any) {
    w.Header().Set("Content-Type","application/json"); w.WriteHeader(status); _=json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter,status int,code,message string) {
    writeJSON(w,status,map[string]any{"error":map[string]string{"code":code,"message":message}})
}
func requestLog(logger *slog.Logger,next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
        start:=time.Now(); next.ServeHTTP(w,r)
        logger.Info("http_request","method",r.Method,"path",r.URL.Path,"duration_ms",time.Since(start).Milliseconds())
    })
}

func storeNotFound(err error) bool { return store.IsNotFound(err) }
func parsePagination(limitValue,offsetValue string)(int,int){ return store.ParsePagination(limitValue,offsetValue) }
