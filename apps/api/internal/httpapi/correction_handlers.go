package httpapi

import (
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/correction"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

type correctDocumentRequest struct {
    correction.Request
    ReviewTaskID string `json:"review_task_id,omitempty"`
}

func (s *Server) getReviewTask(w http.ResponseWriter,r *http.Request){
    task,err:=s.store.GetReviewTask(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","review task not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read review task"); return }
    writeJSON(w,http.StatusOK,task)
}

func (s *Server) correctDocument(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    documentID:=r.PathValue("id")
    actor:=actorFrom(r)

    var req correctDocumentRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,4<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_CORRECTION","invalid JSON body"); return
    }
    if len(req.FieldUpdates)==0 && !req.ReplaceLineItems {
        writeError(w,http.StatusBadRequest,"INVALID_CORRECTION","field_updates or replace_line_items is required"); return
    }

    artifact,err:=s.store.LatestCanonicalArtifact(r.Context(),tenantID,documentID)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","canonical document not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to locate canonical document"); return }

    before,err:=os.ReadFile(artifact.StorageURI)
    if err!=nil { writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to read canonical document"); return }

    latest,err:=s.store.LatestDocumentVersionInfo(r.Context(),tenantID,documentID)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read document version"); return }

    corrected,validation,err:=correction.Apply(before,req.Request,actor)
    if err!=nil { writeError(w,http.StatusBadRequest,"CORRECTION_FAILED",err.Error()); return }
    validationBytes,err:=json.MarshalIndent(validation,"","  ")
    if err!=nil { writeError(w,http.StatusInternalServerError,"VALIDATION_FAILED","failed to encode validation"); return }

    versionID:=newUUID()
    dir:=filepath.Join(s.storage.Root,"tenants",tenantID,"documents",documentID,versionID)
    if err:=os.MkdirAll(dir,0o750); err!=nil {
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to create corrected version folder"); return
    }
    canonicalPath:=filepath.Join(dir,"canonical.json")
    validationPath:=filepath.Join(dir,"validation.json")
    if err:=os.WriteFile(canonicalPath,corrected,0o640); err!=nil {
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to write corrected canonical document"); return
    }
    if err:=os.WriteFile(validationPath,validationBytes,0o640); err!=nil {
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to write corrected validation"); return
    }
    canonicalPath,_=filepath.Abs(canonicalPath)
    validationPath,_=filepath.Abs(validationPath)

    afterAudit,_:=json.Marshal(map[string]any{
        "field_updates":req.FieldUpdates,
        "replace_line_items":req.ReplaceLineItems,
        "reason":req.Reason,
        "new_version_id":versionID,
        "validation_valid":validation.Valid,
    })

    versionNumber,err:=s.store.CommitHumanCorrection(r.Context(),store.CorrectionCommit{
        VersionID:versionID,TenantID:tenantID,DocumentID:documentID,SourceVersionID:latest.ID,
        PipelineVersion:"human-correction-v1",MappingTemplateID:latest.MappingTemplateID,
        MappingTemplateVersion:latest.MappingTemplateVersion,CorrectionReason:req.Reason,CorrectedBy:actor,
        CanonicalPath:canonicalPath,CanonicalSize:int64(len(corrected)),
        ValidationPath:validationPath,ValidationSize:int64(len(validationBytes)),
        RequiresReview:validation.RequiresReview,ReviewTaskID:req.ReviewTaskID,
        AuditBefore:before,AuditAfter:afterAudit,
    })
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to commit corrected version"); return }

    writeJSON(w,http.StatusCreated,map[string]any{
        "document_id":documentID,
        "document_version_id":versionID,
        "version_number":versionNumber,
        "validation":validation,
        "status":func() string {
            if validation.RequiresReview { return "REVIEW_REQUIRED" }
            return "READY_FOR_RECONCILIATION"
        }(),
    })
}
