package httpapi

import (
    "encoding/json"
    "net/http"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/auth"
)

type createAPIKeyRequest struct {
    Name string `json:"name"`
    Role string `json:"role"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) createAPIKey(w http.ResponseWriter,r *http.Request){
    var req createAPIKeyRequest
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&req); err!=nil { writeError(w,http.StatusBadRequest,"INVALID_API_KEY","invalid JSON body"); return }
    req.Name=strings.TrimSpace(req.Name)
    req.Role=strings.ToUpper(strings.TrimSpace(req.Role))
    if req.Name=="" { writeError(w,http.StatusBadRequest,"INVALID_API_KEY","name is required"); return }
    switch req.Role { case "ADMIN","OPERATOR","REVIEWER","READER": default:
        writeError(w,http.StatusBadRequest,"INVALID_API_KEY","unsupported role"); return
    }
    token,prefix,err:=auth.GenerateAPIKey()
    if err!=nil { writeError(w,http.StatusInternalServerError,"KEY_GENERATION_FAILED","failed to generate API key"); return }
    info,err:=s.store.CreateAPIKey(r.Context(),tenantFrom(r,s.defaultTenantID),req.Name,prefix,auth.HashToken(token,s.apiKeyPepper),req.Role,actorFrom(r),req.ExpiresAt)
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to create API key"); return }
    writeJSON(w,http.StatusCreated,map[string]any{"api_key":info,"token":token,"warning":"The token is returned only once. Store it securely."})
}

func (s *Server) listAPIKeys(w http.ResponseWriter,r *http.Request){
    items,err:=s.store.ListAPIKeys(r.Context(),tenantFrom(r,s.defaultTenantID))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list API keys"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":items})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter,r *http.Request){
    if err:=s.store.RevokeAPIKey(r.Context(),tenantFrom(r,s.defaultTenantID),r.PathValue("id")); err!=nil {
        writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to revoke API key"); return
    }
    writeJSON(w,http.StatusOK,map[string]any{"status":"REVOKED"})
}
