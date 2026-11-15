package httpapi

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "log/slog"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/mapping"
    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/storage"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

const maxUploadBytes = 50 << 20

type Server struct {
    store *store.Store
    storage *storage.Local
    queue *queue.Publisher
    defaultTenantID string
    logger *slog.Logger
    invoiceMappings *mapping.Registry
}

type MappingPreviewRequest struct {
    Headers []string `json:"headers"`
    Samples map[string][]string `json:"samples,omitempty"`
}

func New(st *store.Store, fs *storage.Local, q *queue.Publisher, defaultTenantID string, logger *slog.Logger) http.Handler {
    s := &Server{
        store:st,storage:fs,queue:q,defaultTenantID:defaultTenantID,logger:logger,
        invoiceMappings:mapping.DefaultInvoiceRegistry(),
    }
    mux := http.NewServeMux()
    mux.HandleFunc("GET /health/live", s.live)
    mux.HandleFunc("GET /health/ready", s.ready)
    mux.HandleFunc("POST /api/v1/documents", s.upload)
    mux.HandleFunc("GET /api/v1/documents/{id}", s.getDocument)
    mux.HandleFunc("GET /api/v1/documents/{id}/result", s.getDocumentResult)
    mux.HandleFunc("GET /api/v1/documents/{id}/artifacts", s.getDocumentArtifacts)
    mux.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
    mux.HandleFunc("POST /api/v1/mapping-templates/preview", s.previewMapping)
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
    tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
    if tenantID == "" { tenantID = s.defaultTenantID }

    documentID,jobID,runID := newUUID(),newUUID(),newUUID()
    saved,err := s.storage.SaveOriginal(tenantID,documentID,header.Filename,file)
    if err != nil {
        s.logger.Error("store original","error",err,"document_id",documentID)
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to store original"); return
    }
    if err := s.store.CreateDocumentAndJob(r.Context(),store.CreateDocumentParams{
        DocumentID:documentID,TenantID:tenantID,OriginalName:filepath.Base(header.Filename),
        MimeType:mimeType,SHA256:saved.SHA256,StorageURI:saved.Path,JobID:jobID,RunID:runID,
    }); err != nil {
        s.logger.Error("create document","error",err,"document_id",documentID)
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create document"); return
    }
    msg := queue.JobMessage{
        SchemaVersion:"1.0",JobID:jobID,TenantID:tenantID,DocumentID:documentID,
        RunID:runID,Step:"PREPROCESS",InputArtifact:saved.Path,
    }
    if err := s.queue.Publish("document.preprocess",msg); err != nil {
        s.logger.Error("publish preprocess","error",err,"document_id",documentID,"job_id",jobID)
        writeError(w,http.StatusServiceUnavailable,"QUEUE_FAILED","document stored but job could not be queued"); return
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

    data,err:=os.ReadFile(artifact.StorageURI)
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
    })
}

func tenantFrom(r *http.Request,fallback string) string {
    if v := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); v != "" { return v }
    return fallback
}
func allowedUpload(h *multipart.FileHeader,mimeType string) bool {
    switch strings.ToLower(filepath.Ext(filepath.Base(h.Filename))) {
    case ".pdf",".png",".jpg",".jpeg",".tif",".tiff",".csv",".xlsx": return true
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
