package validation

import (
    "encoding/json"
    "testing"
)

func TestMissingTaxDoesNotCreateFalseFailure(t *testing.T){
    payload:=map[string]any{
        "supplier_name":map[string]any{"value":"ABC"},
        "invoice_number":map[string]any{"value":"INV-1"},
        "invoice_date":map[string]any{"value":"2026-11-16"},
        "currency":map[string]any{"value":"PHP"},
        "subtotal":map[string]any{"value":"100.00"},
        "grand_total":map[string]any{"value":"112.00"},
        "line_items":[]map[string]any{
            {"description":"Paper","quantity":"2","unit_price":"50.00","line_total":"100.00"},
        },
    }
    data,_:=json.Marshal(payload)
    result,err:=ValidateInvoice(data,"0.01")
    if err!=nil { t.Fatal(err) }
    if !result.Valid || result.RequiresReview { t.Fatalf("unexpected failure: %#v",result) }
    foundWarning:=false
    for _,rule:=range result.Rules {
        if rule.Code=="TAX_NOT_AVAILABLE_FOR_TOTAL_CHECK" && rule.Status=="WARN" {
            foundWarning=true
        }
    }
    if !foundWarning { t.Fatal("expected missing-tax warning") }
}
