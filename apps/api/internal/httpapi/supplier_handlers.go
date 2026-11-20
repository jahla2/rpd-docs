package httpapi

import (
    "encoding/json"
    "net/http"
    "strconv"
    "strings"
)

type createSupplierRequest struct {
    SupplierName string `json:"supplier_name"`
    SupplierCode string `json:"supplier_code,omitempty"`
    TaxID string `json:"tax_id,omitempty"`
}

type createSupplierAliasRequest struct {
    Alias string `json:"alias"`
}

func (s *Server) createSupplier(w http.ResponseWriter,r *http.Request){
    var req createSupplierRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_SUPPLIER","invalid JSON body"); return
    }
    supplier,err:=s.store.CreateSupplier(
        r.Context(),tenantFrom(r,s.defaultTenantID),req.SupplierName,req.SupplierCode,req.TaxID)
    if err!=nil { writeError(w,http.StatusBadRequest,"SUPPLIER_CREATE_FAILED",err.Error()); return }
    writeJSON(w,http.StatusCreated,supplier)
}

func (s *Server) addSupplierAlias(w http.ResponseWriter,r *http.Request){
    var req createSupplierAliasRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil {
        writeError(w,http.StatusBadRequest,"INVALID_SUPPLIER_ALIAS","invalid JSON body"); return
    }
    alias,err:=s.store.AddSupplierAlias(
        r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id"),req.Alias)
    if err!=nil { writeError(w,http.StatusBadRequest,"SUPPLIER_ALIAS_FAILED",err.Error()); return }
    writeJSON(w,http.StatusCreated,alias)
}

func (s *Server) searchSuppliers(w http.ResponseWriter,r *http.Request){
    limit:=25
    if parsed,err:=strconv.Atoi(r.URL.Query().Get("limit")); err==nil && parsed>0 && parsed<=100 { limit=parsed }
    items,err:=s.store.SearchSuppliers(
        r.Context(),tenantFrom(r,s.defaultTenantID),strings.TrimSpace(r.URL.Query().Get("q")),limit)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to search suppliers"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}
