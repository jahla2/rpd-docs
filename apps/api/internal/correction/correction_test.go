package correction

import (
    "encoding/json"
    "testing"
)

func TestApplyCorrectionCreatesHumanEvidence(t *testing.T){
    original:=[]byte(`{
      "supplier_name":{"value":"ABC"},
      "invoice_number":{"value":"INV-1"},
      "invoice_date":{"value":"2026-11-16"},
      "currency":{"value":"PHP"},
      "subtotal":{"value":"100.00"},
      "tax":{"value":"12.00"},
      "grand_total":{"value":"112.00"},
      "line_items":[{"quantity":"2","unit_price":"50.00","line_total":"100.00"}]
    }`)
    updated,validation,err:=Apply(original,Request{
        FieldUpdates:map[string]string{"supplier_name":"ABC Trading Ltd"},
        Reason:"reviewed supplier name",
    },"reviewer-1")
    if err!=nil { t.Fatal(err) }
    if !validation.Valid { t.Fatalf("validation=%#v",validation) }
    var doc map[string]any
    if err:=json.Unmarshal(updated,&doc); err!=nil { t.Fatal(err) }
    field:=doc["supplier_name"].(map[string]any)
    evidence:=field["evidence"].(map[string]any)
    if field["value"]!="ABC Trading Ltd" || evidence["method"]!="human_correction" {
        t.Fatalf("doc=%#v",doc)
    }
}

func TestInvalidCorrectionFieldRejected(t *testing.T){
    _,_,err:=Apply([]byte(`{"line_items":[]}`),Request{
        FieldUpdates:map[string]string{"system_prompt":"bad"},
    },"reviewer")
    if err==nil { t.Fatal("expected rejected field") }
}
