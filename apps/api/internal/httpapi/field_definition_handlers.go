package httpapi

import (
    "encoding/json"
    "errors"
    "net/http"
    "regexp"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}package httpapi

import (
    "encoding/json"
    "errors"
    "net/http"
    "regexp"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

)
var fieldDefinitionUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}package httpapi

import (
    "encoding/json"
    "errors"
    "net/http"
    "regexp"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

)

var allowedFieldDataTypes = map[string]bool{
    "string": true,
    "integer": true,
    "decimal": true,
    "date": true,
    "datetime": true,
    "currency": true,
    "percentage": true,
    "boolean": true,
    "identifier": true,
    "email": true,
    "phone": true,
    "unknown": true,
}

type fieldDefinitionRequest struct {
    DocumentTypeID string `json:"document_type_id"`
    FieldName      string `json:"field_name"`
    DisplayName    string `json:"display_name"`
    DataType       string `json:"data_type"`
    Required       bool   `json:"required"`
    Repeating      bool   `json:"repeating"`
}

func validateFieldDefinitionRequest(req fieldDefinitionRequest) (fieldDefinitionRequest, string) {
    req.DocumentTypeID = strings.TrimSpace(req.DocumentTypeID)
    if req.DocumentTypeID == "" {
        return fieldDefinitionRequest{}, "document_type_id is required"
    }
    if !fieldDefinitionUUIDPattern.MatchString(req.DocumentTypeID) {
        return fieldDefinitionRequest{}, "document_type_id must be a valid UUID"
    }

    req.FieldName = strings.ToLower(strings.TrimSpace(req.FieldName))
    req.FieldName = strings.ReplaceAll(req.FieldName, "-", "_")
    req.FieldName = strings.ReplaceAll(req.FieldName, " ", "_")
    if !fieldNamePattern.MatchString(req.FieldName) {
        return fieldDefinitionRequest{}, "field_name must use lowercase snake_case and contain at most 64 characters"
    }

    req.DisplayName = strings.TrimSpace(req.DisplayName)
    if req.DisplayName == "" || len(req.DisplayName) > 200 {
        return fieldDefinitionRequest{}, "display_name must contain between 1 and 200 characters"
    }

    req.DataType = strings.ToLower(strings.TrimSpace(req.DataType))
    if !allowedFieldDataTypes[req.DataType] {
        return fieldDefinitionRequest{}, "data_type is not supported"
    }

    return req, ""
}

func (s *Server) listFieldDefinitions(w http.ResponseWriter, r *http.Request) {
    documentTypeID := strings.TrimSpace(r.URL.Query().Get("document_type_id"))
    if documentTypeID != "" && !fieldDefinitionUUIDPattern.MatchString(documentTypeID) {
        writeError(w, http.StatusBadRequest, "INVALID_DOCUMENT_TYPE_ID", "document_type_id must be a valid UUID")
        return
    }
    items, err := s.store.ListFieldDefinitions(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        documentTypeID,
    )
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to list field definitions")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createFieldDefinition(w http.ResponseWriter, r *http.Request) {
    var req fieldDefinitionRequest
    decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_FIELD_DEFINITION", "invalid JSON body")
        return
    }

    req, validationError := validateFieldDefinitionRequest(req)
    if validationError != "" {
        writeError(w, http.StatusBadRequest, "INVALID_FIELD_DEFINITION", validationError)
        return
    }

    item, err := s.store.CreateFieldDefinition(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        req.DocumentTypeID,
        req.FieldName,
        req.DisplayName,
        req.DataType,
        req.Required,
        req.Repeating,
        actorFrom(r),
    )
    if store.IsNotFound(err) {
        writeError(w, http.StatusBadRequest, "DOCUMENT_TYPE_NOT_FOUND", "document type not found")
        return
    }
    if errors.Is(err, store.ErrFieldDefinitionDocumentTypeInactive) {
        writeError(w, http.StatusConflict, "DOCUMENT_TYPE_INACTIVE", "field definitions cannot be added to an inactive document type")
        return
    }
    if store.IsFieldDefinitionConflict(err) {
        writeError(w, http.StatusConflict, "FIELD_DEFINITION_EXISTS", "field definition already exists for this document type")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to create field definition")
        return
    }
    writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateFieldDefinition(w http.ResponseWriter, r *http.Request) {
    var req fieldDefinitionRequest
    decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "INVALID_FIELD_DEFINITION", "invalid JSON body")
        return
    }

    req, validationError := validateFieldDefinitionRequest(req)
    if validationError != "" {
        writeError(w, http.StatusBadRequest, "INVALID_FIELD_DEFINITION", validationError)
        return
    }

    item, err := s.store.UpdateFieldDefinition(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        r.PathValue("id"),
        req.DocumentTypeID,
        req.FieldName,
        req.DisplayName,
        req.DataType,
        req.Required,
        req.Repeating,
        actorFrom(r),
    )
    if store.IsNotFound(err) {
        writeError(w, http.StatusNotFound, "NOT_FOUND", "field definition not found")
        return
    }
    if errors.Is(err, store.ErrFieldDefinitionIdentityImmutable) {
        writeError(w, http.StatusConflict, "FIELD_DEFINITION_IDENTITY_IMMUTABLE", "document_type_id and field_name cannot be changed after creation")
        return
    }
    if store.IsFieldDefinitionConflict(err) {
        writeError(w, http.StatusConflict, "FIELD_DEFINITION_EXISTS", "field definition already exists for this document type")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to update field definition")
        return
    }
    writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteFieldDefinition(w http.ResponseWriter, r *http.Request) {
    err := s.store.DeleteFieldDefinition(
        r.Context(),
        tenantFrom(r, s.defaultTenantID),
        r.PathValue("id"),
        actorFrom(r),
    )
    if store.IsNotFound(err) {
        writeError(w, http.StatusNotFound, "NOT_FOUND", "field definition not found")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to delete field definition")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
