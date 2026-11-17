package pipeline

import "testing"

func TestNormalizeSupplierLookup(t *testing.T){
    if got:=normalizeSupplierLookup("  ABC Trading Ltd. "); got!="ABCTRADINGLTD" {
        t.Fatalf("normalized=%q",got)
    }
    if got:=normalizeSupplierLookup("A-B_C/123"); got!="ABC123" {
        t.Fatalf("normalized=%q",got)
    }
}
