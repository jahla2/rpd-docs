package httpapi

import (
    "fmt"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/exporter"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

func (s *Server) exportReconciliationRun(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    runID:=r.PathValue("id")
    format:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
    profileID:=strings.TrimSpace(r.URL.Query().Get("profile_id"))
    var profile *store.OutputProfile
    if profileID!="" {
        selected,profileErr:=s.store.GetOutputProfile(r.Context(),tenantID,profileID)
        if storeNotFound(profileErr) { writeError(w,http.StatusNotFound,"NOT_FOUND","output profile not found"); return }
        if profileErr!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read output profile"); return }
        if !selected.Active { writeError(w,http.StatusConflict,"OUTPUT_PROFILE_INACTIVE","output profile is inactive"); return }
        profile=&selected
        format=selected.Format
    }
    if format=="" { format="xlsx" }

    run,err:=s.store.GetReconciliationRun(r.Context(),tenantID,runID)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","reconciliation run not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read reconciliation run"); return }

    matches,err:=s.store.ListMatches(r.Context(),tenantID,runID)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read matches"); return }
    exceptions,err:=s.store.ListExceptions(r.Context(),tenantID,runID)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read exceptions"); return }

    report:=exporter.Report{Run:run,Matches:matches,Exceptions:exceptions}
    var data []byte
    var contentType,ext string
    if profile!=nil {
        data,contentType,ext,err=exporter.BuildWithProfile(exporter.Profile{
            Format:profile.Format,Columns:profile.Columns,HeaderAliases:profile.HeaderAliases,
        },report)
    } else {
        data,contentType,ext,err=exporter.Build(format,report)
    }
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
