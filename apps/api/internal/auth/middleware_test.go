package auth

import (
    "net/http"
    "testing"
)

func TestRequiredPermissionForDocumentTypes(t *testing.T) {
    tests := []struct {
        method string
        path string
        want string
    }{
        {http.MethodGet, "/api/v1/document-types", "config:read"},
        {http.MethodGet, "/api/v1/document-types/abc", "config:read"},
        {http.MethodPost, "/api/v1/document-types", "config:write"},
        {http.MethodPut, "/api/v1/document-types/abc", "config:write"},
    }

    for _, test := range tests {
        if got := requiredPermission(test.method, test.path); got != test.want {
            t.Fatalf("%s %s permission=%q want=%q", test.method, test.path, got, test.want)
        }
    }
}
