package httpapi

import (
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/exporter"
)

func (s *Server) exportReconciliationRun(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    runID:=r.PathValue("id")
    format:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
    if format=="" { format="xlsx" }

    run,err:=s.store.GetReconciliationRun(r.Context(),tenantID,runID)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","reconciliation run not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read reconciliation run"); return }

    matches,err:=s.store.ListMatches(r.Context(),tenantID,runID)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read matches"); return }
    exceptions,err:=s.store.ListExceptions(r.Context(),tenantID,runID)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read exceptions"); return }

    data,contentType,ext,err:=exporter.Build(format,exporter.Report{Run:run,Matches:matches,Exceptions:exceptions})
    if err!=nil { writeError(w,http.StatusBadRequest,"INVALID_EXPORT_FORMAT",err.Error()); return }

    exportID:=newUUID()
    dir:=filepath.Join(s.storage.Root,"tenants",tenantID,"exports",exportID)
    if err:=os.MkdirAll(dir,0o750); err!=nil {
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to create export folder"); return
    }
    path:=filepath.Join(dir,"result."+ext)
    if err:=os.WriteFile(path,data,0o640); err!=nil {
        writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to write export"); return
    }
    absolutePath,err:=filepath.Abs(path)
    if err!=nil { writeError(w,http.StatusInternalServerError,"STORAGE_FAILED","failed to resolve export path"); return }

    saved,err:=s.store.CreateExport(r.Context(),exportID,tenantID,runID,format,absolutePath,contentType,int64(len(data)))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to register export"); return }

    writeJSON(w,http.StatusCreated,map[string]any{
        "export":saved,
        "download_url":fmt.Sprintf("/api/v1/exports/%s/download",saved.ID),
    })
}

func (s *Server) getExport(w http.ResponseWriter,r *http.Request){
    export,err:=s.store.GetExport(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","export not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read export"); return }
    writeJSON(w,http.StatusOK,map[string]any{
        "export":export,
        "download_url":fmt.Sprintf("/api/v1/exports/%s/download",export.ID),
    })
}

func (s *Server) downloadExport(w http.ResponseWriter,r *http.Request){
    export,err:=s.store.GetExport(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","export not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read export"); return }

    if _,err:=os.Stat(export.StorageURI); err!=nil {
        writeError(w,http.StatusNotFound,"EXPORT_FILE_MISSING","export file is unavailable"); return
    }
    w.Header().Set("Content-Type",export.ContentType)
    w.Header().Set("Content-Disposition",fmt.Sprintf("attachment; filename=%q","reconciliation."+export.Format))
    http.ServeFile(w,r,export.StorageURI)
}
