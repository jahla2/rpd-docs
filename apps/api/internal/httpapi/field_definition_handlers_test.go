package httpapi

import "testing"

func TestValidateFieldDefinitionRequest(t *testing.T) {
    got, validationError := validateFieldDefinitionRequest(fieldDefinitionRequest{
        DocumentTypeID: "00000000-0000-0000-0000-000000000001",
        FieldName: "Invoice Number",
        DisplayName: "Invoice Number",
        DataType: "IDENTIFIER",
        Required: true,
    })
    if validationError != "" {
        t.Fatalf("unexpected validation error: %s", validationError)
    }
    if got.FieldName != "invoice_number" || got.DataType != "identifier" {
        t.Fatalf("unexpected normalized request: %+v", got)
    }
}

func TestValidateFieldDefinitionRequestRejectsUnsafeNameAndType(t *testing.T) {
    _, validationError := validateFieldDefinitionRequest(fieldDefinitionRequest{
        DocumentTypeID: "00000000-0000-0000-0000-000000000001",
        FieldName: "../invoice",
        DisplayName: "Invoice",
        DataType: "shell",
    })
    if validationError == "" {
        t.Fatal("expected invalid field name to be rejected")
    }

    _, validationError = validateFieldDefinitionRequest(fieldDefinitionRequest{
        DocumentTypeID: "00000000-0000-0000-0000-000000000001",
        FieldName: "invoice_number",
        DisplayName: "Invoice",
        DataType: "shell",
    })
    if validationError == "" {
        t.Fatal("expected unsupported data type to be rejected")
    }
}
