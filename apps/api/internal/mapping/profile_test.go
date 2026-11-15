package mapping

import "testing"

func TestProfileColumnNumeric(t *testing.T){
    p:=ProfileColumn("Amount",[]string{"1,250.00","900.00","","100"})
    if p.NonEmptyCount!=3 || p.EmptyCount!=1 { t.Fatalf("profile=%#v",p) }
    if p.NumericRatio!=1 || p.InferredType!="decimal" { t.Fatalf("profile=%#v",p) }
}

func TestProfileColumnIdentifier(t *testing.T){
    p:=ProfileColumn("Invoice No",[]string{"INV-001","INV-002","INV-003"})
    if p.InferredType!="identifier" { t.Fatalf("profile=%#v",p) }
}
