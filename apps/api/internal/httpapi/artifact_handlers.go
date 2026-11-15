package httpapi

import (
    "net/http"
    "strings"
    "time"
)

func (s *Server) getArtifactAccess(w http.ResponseWriter,r *http.Request){
    artifact,err:=s.store.GetArtifact(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","artifact not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read artifact"); return }

    if strings.HasPrefix(artifact.StorageURI,"s3://") {
        signed,err:=s.storage.PresignGet(artifact.StorageURI,15*time.Minute)
        if err!=nil { writeError(w,http.StatusServiceUnavailable,"STORAGE_FAILED","failed to sign artifact URL"); return }
        writeJSON(w,http.StatusOK,map[string]any{
            "artifact_id":artifact.ID,"url":signed,"expires_in_seconds":900,
        })
        return
    }
    writeJSON(w,http.StatusOK,map[string]any{
        "artifact_id":artifact.ID,
        "url":"/api/v1/artifacts/"+artifact.ID+"/content",
        "expires_in_seconds":nil,
    })
}

func (s *Server) getArtifactContent(w http.ResponseWriter,r *http.Request){
    artifact,err:=s.store.GetArtifact(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","artifact not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read artifact"); return }
    data,err:=s.storage.Read(artifact.StorageURI)
    if err!=nil { writeError(w,http.StatusNotFound,"STORAGE_FAILED","artifact bytes are unavailable"); return }
    contentType:="application/octet-stream"
    if artifact.ContentType!=nil && *artifact.ContentType!="" { contentType=*artifact.ContentType }
    w.Header().Set("Content-Type",contentType)
    w.WriteHeader(http.StatusOK)
    _,_=w.Write(data)
}
