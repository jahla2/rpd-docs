package mapping

import "testing"

func TestNormalizeHeader(t *testing.T) {
    if got:=NormalizeHeader("  Invoice_No. "); got!="invoice no" {
        t.Fatalf("got %q",got)
    }
}

func TestDefaultInvoiceSuggestions(t *testing.T) {
    got:=DefaultInvoiceRegistry().Suggest([]string{"Inv Ref","Qty","Extended Amount"})
    want:=[]string{"invoice_number","quantity","line_total"}
    for i,s:=range got {
        if s.Target!=want[i] { t.Fatalf("suggestion %d target=%q want=%q",i,s.Target,want[i]) }
        if s.Confidence<0.9 { t.Fatalf("suggestion %d confidence=%f",i,s.Confidence) }
    }
}

func TestInferType(t *testing.T) {
    if got:=InferType([]string{"1,200.50","99.00"}); got!="decimal" { t.Fatalf("decimal got %q",got) }
    if got:=InferType([]string{"2026-11-16","2026-11-17"}); got!="date" { t.Fatalf("date got %q",got) }
}
