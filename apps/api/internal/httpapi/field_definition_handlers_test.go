package httpapi

import "testing"

func TestValidateFieldDefinitionRequest(t *testing.T) {
    got, validationError := validateFieldDefinitionRequest(fieldDefinitionRequest{
        DocumentTypeID: "123e4567-e89b-42d3-a456-426614174000",
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
        DocumentTypeID: "123e4567-e89b-42d3-a456-426614174000",
        FieldName: "../invoice",
        DisplayName: "Invoice",
        DataType: "shell",
    })
    if validationError == "" {
        t.Fatal("expected invalid field name to be rejected")
    }

    _, validationError = validateFieldDefinitionRequest(fieldDefinitionRequest{
        DocumentTypeID: "123e4567-e89b-42d3-a456-426614174000",
        FieldName: "invoice_number",
        DisplayName: "Invoice",
        DataType: "shell",
    })
    if validationError == "" {
        t.Fatal("expected unsupported data type to be rejected")
    }
}
