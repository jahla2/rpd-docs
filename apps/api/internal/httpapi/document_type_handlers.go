package httpapi

import (
    "encoding/json"
    "errors"
    "net/http"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

type documentTypeRequest struct {
    Code          string `json:"code"`
    Name          string `json:"name"`
    SchemaVersion int    `json:"schema_version"`
    Active        *bool  `json:"active,omitempty"`
}

func validateDocumentTypeRequest(req documentTypeRequest, requireActive bool) (string, string, int, bool, string) {
    code, ok := normalizeDocumentType(req.Code)
    if !ok {
        return "", "", 0, false, "code must be a supported document type"
    }

    name := strings.TrimSpace(req.Name)
    if name == "" || len(name) > 200 {
        return "", "", 0, false, "name must contain between 1 and 200 characters"
    }

    schemaVersion := req.SchemaVersion
    if schemaVersion == 0 {
        schemaVersion = 1
    }
    if schemaVersion < 1 || schemaVersion > 1000 {
        return "", "", 0, false, "schema_version must be between 1 and 1000"
    }

    if requireActive && req.Active == nil {
        return "", "", 0, false, "active is required for a full update"
    }
    active := true
    if req.Active != nil {
        active = *req.Active
    }

    return code, name, schemaVersion, active, ""
}

func (s *Server) listDocumentTypes(w http.ResponseWriter, r *http.Request) {
    items, err := s.store.ListDocumentTypes(r.Context(), tenantFrom(r, s.defaultTenantID))
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to list document types")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createDocumentType(w http.ResponseWriter, r *http.Request) {
    var req documentTypeRequest
    decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE", "invalid JSON body")
        return
    }

    code, name, schemaVersion, active, validationError := validateDocumentTypeRequest(req, false)
    if validationError != "" {
        writeError(w, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE", validationError)
        return
    }

    item, err := s.store.CreateDocumentType(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        code,
        name,
        schemaVersion,
        active,
        actorFrom(r),
    )
    if store.IsDocumentTypeConflict(err) {
        writeError(w, http.StatusConflict, "DOCUMENT_TYPE_EXISTS", "document type code and schema version already exist")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to create document type")
        return
    }
    writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getDocumentType(w http.ResponseWriter, r *http.Request) {
    item, err := s.store.GetDocumentType(r.Context(), tenantFrom(r, s.defaultTenantID), r.PathValue("id"))
    if store.IsNotFound(err) {
        writeError(w, http.StatusNotFound, "NOT_FOUND", "document type not found")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to read document type")
        return
    }
    writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateDocumentType(w http.ResponseWriter, r *http.Request) {
    var req documentTypeRequest
    decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE", "invalid JSON body")
        return
    }

    code, name, schemaVersion, active, validationError := validateDocumentTypeRequest(req, true)
    if validationError != "" {
        writeError(w, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE", validationError)
        return
    }

    item, err := s.store.UpdateDocumentType(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        r.PathValue("id"),
        code,
        name,
        schemaVersion,
        active,
        actorFrom(r),
    )
    if store.IsNotFound(err) {
        writeError(w, http.StatusNotFound, "NOT_FOUND", "document type not found")
        return
    }
    if store.IsDocumentTypeConflict(err) {
        writeError(w, http.StatusConflict, "DOCUMENT_TYPE_EXISTS", "document type code and schema version already exist")
        return
    }
    if errors.Is(err, store.ErrDocumentTypeIdentityImmutable) {
        writeError(w, http.StatusConflict, "DOCUMENT_TYPE_IDENTITY_IMMUTABLE", "code and schema_version cannot be changed after creation")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to update document type")
        return
    }
    writeJSON(w, http.StatusOK, item)
}
