package auth

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

type Middleware struct {
    Store *store.Store
    Mode string
    Pepper string
    Logger *slog.Logger
    Limiter *RateLimiter
}

func (m Middleware) Wrap(next http.Handler) http.Handler {
    if strings.EqualFold(strings.TrimSpace(m.Mode),"disabled") {
        return next
    }
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if strings.HasPrefix(r.URL.Path,"/health/") {
            next.ServeHTTP(w,r); return
        }
        token:=bearer(r.Header.Get("Authorization"))
        if token=="" { token=strings.TrimSpace(r.Header.Get("X-API-Key")) }
        prefix,err:=ParseAPIKey(token)
        if err!=nil { authError(w,http.StatusUnauthorized,"UNAUTHORIZED","valid API key required"); return }

        key,err:=m.Store.GetAPIKeyAuthByPrefix(r.Context(),prefix)
        if err!=nil || key.Status!="ACTIVE" {
            authError(w,http.StatusUnauthorized,"UNAUTHORIZED","API key is invalid or inactive"); return
        }
        if key.ExpiresAt!=nil && time.Now().UTC().After(*key.ExpiresAt) {
            authError(w,http.StatusUnauthorized,"API_KEY_EXPIRED","API key has expired"); return
        }
        if !VerifyToken(token,m.Pepper,key.SecretHash) {
            authError(w,http.StatusUnauthorized,"UNAUTHORIZED","API key is invalid"); return
        }
        permissions:=make(map[string]bool,len(key.Permissions))
        for _,permission:=range key.Permissions { permissions[permission]=true }
        required:=requiredPermission(r.Method,r.URL.Path)
        if required!="" && !permissions[required] {
            authError(w,http.StatusForbidden,"FORBIDDEN","API key does not have the required permission"); return
        }
        if m.Limiter!=nil && !m.Limiter.Allow(key.ID) {
            w.Header().Set("Retry-After","1")
            authError(w,http.StatusTooManyRequests,"RATE_LIMITED","API key request rate exceeded")
            return
        }

        principal:=Principal{
            APIKeyID:key.ID,TenantID:key.TenantID,ActorID:"api-key:"+key.ID,
            Role:key.Role,Permissions:permissions,
        }
        m.Store.TouchAPIKey(r.Context(),key.ID)
        next.ServeHTTP(w,r.WithContext(WithPrincipal(r.Context(),principal)))
    })
}

func requiredPermission(method,path string) string {
    if strings.HasPrefix(path,"/api/v1/admin/") { return "admin:keys" }
    if strings.HasPrefix(path,"/api/v1/review-tasks") { return permissionByMethod(method,"document:read","review:write") }
    if strings.Contains(path,"/corrections") { return "review:write" }
    if strings.Contains(path,"/matches/") && (strings.HasSuffix(path,"/approve")||strings.HasSuffix(path,"/reject")) { return "review:write" }
    if strings.HasPrefix(path,"/api/v1/reconciliation-runs") { return permissionByMethod(method,"reconciliation:read","reconciliation:write") }
    if strings.HasPrefix(path,"/api/v1/exports") { return "reconciliation:read" }
    if strings.HasPrefix(path,"/api/v1/artifacts") { return "document:read" }
    if strings.HasPrefix(path,"/api/v1/output-profiles") { return permissionByMethod(method,"config:read","config:write") }
    if strings.HasPrefix(path,"/api/v1/document-types") || strings.HasPrefix(path,"/api/v1/datasets") || strings.HasPrefix(path,"/api/v1/mapping-templates") || strings.HasPrefix(path,"/api/v1/suppliers") {
        return permissionByMethod(method,"config:read","config:write")
    }
    if strings.HasPrefix(path,"/api/v1/documents") || strings.HasPrefix(path,"/api/v1/jobs") {
        return permissionByMethod(method,"document:read","document:write")
    }
    return ""
}

func permissionByMethod(method,read,write string) string {
    if method==http.MethodGet || method==http.MethodHead { return read }
    return write
}

func bearer(value string) string {
    value=strings.TrimSpace(value)
    if len(value)>7 && strings.EqualFold(value[:7],"Bearer ") { return strings.TrimSpace(value[7:]) }
    return ""
}

func authError(w http.ResponseWriter,status int,code,message string){
    w.Header().Set("Content-Type","application/json")
    w.WriteHeader(status)
    _=json.NewEncoder(w).Encode(map[string]any{"error":map[string]string{"code":code,"message":message}})
}
