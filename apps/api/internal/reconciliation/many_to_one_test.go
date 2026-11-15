package reconciliation

import "testing"

func TestFindThreeInvoicesForOnePayment(t *testing.T){
    entries:=[]AmountEntry{
        {ID:"doc-a",Amount:"100.00"},
        {ID:"doc-b",Amount:"200.00"},
        {ID:"doc-c",Amount:"50.00"},
        {ID:"doc-d",Amount:"999.00"},
    }
    got,ok,err:=FindAmountCombination(entries,"350.00","0.01",4)
    if err!=nil { t.Fatal(err) }
    if !ok { t.Fatal("expected grouped source match") }
    if len(got.IDs)!=3 || got.Total!="350.0000" { t.Fatalf("got %#v",got) }
}
