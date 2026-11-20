package reconciliation

import "testing"

func TestFindAmountCombination(t *testing.T){
    entries:=[]AmountEntry{
        {ID:"a",Amount:"40.00"},
        {ID:"b",Amount:"60.00"},
        {ID:"c",Amount:"25.00"},
    }
    got,ok,err:=FindAmountCombination(entries,"100.00","0.01",4)
    if err!=nil { t.Fatal(err) }
    if !ok { t.Fatal("expected combination") }
    if got.Total!="100.0000" || len(got.IDs)!=2 { t.Fatalf("got %#v",got) }
}

func TestFindAmountCombinationUsesTolerance(t *testing.T){
    entries:=[]AmountEntry{{ID:"a",Amount:"49.99"},{ID:"b",Amount:"50.00"}}
    got,ok,err:=FindAmountCombination(entries,"100.00","0.02",3)
    if err!=nil { t.Fatal(err) }
    if !ok || got.Difference!="0.0100" { t.Fatalf("got %#v ok=%v",got,ok) }
}
