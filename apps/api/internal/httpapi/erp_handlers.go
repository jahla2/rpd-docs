package httpapi

import (
    "encoding/json"
    "net/http"
    "path/filepath"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/erp"
)

type createDatasetRequest struct {
    Name string `json:"name"`
    SourceType string `json:"source_type"`
}

func (s *Server) createDataset(w http.ResponseWriter,r *http.Request){
    var req createDatasetRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_DATASET","invalid JSON body"); return
    }
    req.Name=strings.TrimSpace(req.Name)
    req.SourceType=strings.TrimSpace(req.SourceType)
    if req.Name=="" {
        writeError(w,http.StatusBadRequest,"INVALID_DATASET","name is required"); return
    }
    if req.SourceType=="" { req.SourceType="erp" }

    dataset,err:=s.store.CreateDataset(r.Context(),tenantFrom(r,s.defaultTenantID),req.Name,req.SourceType)
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create dataset"); return
    }
    writeJSON(w,http.StatusCreated,dataset)
}

func (s *Server) getDataset(w http.ResponseWriter,r *http.Request){
    dataset,err:=s.store.GetDataset(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"))
    if storeNotFound(err) {
        writeError(w,http.StatusNotFound,"NOT_FOUND","dataset not found"); return
    }
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read dataset"); return
    }
    count,_:=s.store.DatasetRecordCount(r.Context(),tenantFrom(r,s.defaultTenantID),dataset.ID)
    writeJSON(w,http.StatusOK,map[string]any{"dataset":dataset,"record_count":count})
}

func (s *Server) importDataset(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    datasetID:=r.PathValue("id")
    if _,err:=s.store.GetDataset(r.Context(),tenantID,datasetID); err!=nil {
        if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","dataset not found") } else {
            writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read dataset")
        }
        return
    }

    r.Body=http.MaxBytesReader(w,r.Body,100<<20)
    if err:=r.ParseMultipartForm(100<<20); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_IMPORT","invalid multipart upload"); return
    }
    file,header,err:=r.FormFile("file")
    if err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_IMPORT","multipart field 'file' is required"); return
    }
    defer file.Close()

    ext:=strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
    if ext!=".csv" && ext!=".xlsx" {
        writeError(w,http.StatusUnsupportedMediaType,"UNSUPPORTED_FORMAT","ERP import supports CSV and XLSX"); return
    }

    overrides:=map[string]string{}
    if raw:=strings.TrimSpace(r.FormValue("mapping")); raw!="" {
        if err:=json.Unmarshal([]byte(raw),&overrides); err!=nil {
            writeError(w,http.StatusBadRequest,"INVALID_MAPPING","mapping must be a JSON object of source header to canonical field"); return
        }
    }

    result,err:=erp.Parse(header.Filename,file,overrides)
    if err!=nil {
        writeError(w,http.StatusBadRequest,"IMPORT_FAILED",err.Error()); return
    }

    inserted,skipped,err:=s.store.ImportERPRecords(r.Context(),tenantID,datasetID,result.Rows)
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to store ERP records"); return
    }

    writeJSON(w,http.StatusOK,map[string]any{
        "dataset_id":datasetID,
        "headers":result.Headers,
        "mapping":result.Mapping,
        "valid_rows":len(result.Rows),
        "inserted":inserted,
        "duplicates_skipped":skipped,
        "errors":result.Errors,
    })
}

func (s *Server) listDatasetRecords(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    datasetID:=r.PathValue("id")
    limit,offset:=parsePagination(r.URL.Query().Get("limit"),r.URL.Query().Get("offset"))
    records,err:=s.store.ListERPRecords(r.Context(),tenantID,datasetID,limit,offset)
    if err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list ERP records"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{
        "dataset_id":datasetID,"limit":limit,"offset":offset,"items":records,
    })
}
