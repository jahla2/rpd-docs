package auth

import (
    "net/http"
    "testing"
)

func TestRequiredPermissionForFieldDefinitions(t *testing.T) {
    tests := []struct {
        method string
        want string
    }{
        {http.MethodGet, "config:read"},
        {http.MethodPost, "config:write"},
        {http.MethodPut, "config:write"},
        {http.MethodDelete, "config:write"},
    }
    for _, test := range tests {
        if got := requiredPermission(test.method, "/api/v1/field-definitions/abc"); got != test.want {
            t.Fatalf("%s permission=%q want=%q", test.method, got, test.want)
        }
    }
}
