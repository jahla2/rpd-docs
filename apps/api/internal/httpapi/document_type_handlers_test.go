package httpapi

import "testing"

func boolPtr(value bool) *bool { return &value }

func TestValidateDocumentTypeRequest(t *testing.T) {
    code, name, version, active, validationError := validateDocumentTypeRequest(documentTypeRequest{
        Code: "Supplier Statement",
        Name: "Supplier Statement",
    }, false)
    if validationError != "" {
        t.Fatalf("unexpected validation error: %s", validationError)
    }
    if code != "supplier_statement" || name != "Supplier Statement" || version != 1 || !active {
        t.Fatalf("unexpected normalized values: code=%q name=%q version=%d active=%v", code, name, version, active)
    }
}

func TestValidateDocumentTypeRequestRejectsUnsupportedType(t *testing.T) {
    _, _, _, _, validationError := validateDocumentTypeRequest(documentTypeRequest{
        Code: "custom_unwired_type",
        Name: "Custom",
    }, false)
    if validationError == "" {
        t.Fatal("expected unsupported document type to be rejected")
    }
}

func TestValidateDocumentTypeRequestRequiresActiveOnPut(t *testing.T) {
    _, _, _, _, validationError := validateDocumentTypeRequest(documentTypeRequest{
        Code: "invoice",
        Name: "Invoice",
        SchemaVersion: 1,
    }, true)
    if validationError == "" {
        t.Fatal("expected active to be required for a full update")
    }

    _, _, _, active, validationError := validateDocumentTypeRequest(documentTypeRequest{
        Code: "invoice",
        Name: "Invoice",
        SchemaVersion: 1,
        Active: boolPtr(false),
    }, true)
    if validationError != "" {
        t.Fatalf("unexpected validation error: %s", validationError)
    }
    if active {
        t.Fatal("expected false active value to be preserved")
    }
}
