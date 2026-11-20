package reconciliation

import (
    "testing"
    "time"
)

func TestExactMatch(t *testing.T) {
    day:=time.Date(2026,11,16,0,0,0,0,time.UTC)
    source:=Record{ID:"doc1",Supplier:"ABC Trading Ltd.",Reference:"INV-001",Currency:"PHP",Amount:"12500.00",Date:day}
    target:=Record{ID:"erp1",Supplier:"abc trading ltd",Reference:"INV001",Currency:"php",Amount:"12500",Date:day}
    got,err:=Match(source,target,"0.01",3)
    if err!=nil { t.Fatal(err) }
    if got.Status!="MATCHED_EXACT" || got.Score!=100 { t.Fatalf("got %#v",got) }
    if got.MatchingRule!="supplier_reference_amount_currency" { t.Fatalf("rule=%q",got.MatchingRule) }
}

func TestToleranceMatch(t *testing.T) {
    day:=time.Date(2026,11,16,0,0,0,0,time.UTC)
    source:=Record{ID:"doc1",Supplier:"ABC",Reference:"INV-1",Currency:"PHP",Amount:"100.00",Date:day}
    target:=Record{ID:"erp1",Supplier:"ABC",Reference:"INV1",Currency:"PHP",Amount:"100.01",Date:day.AddDate(0,0,1)}
    got,err:=Match(source,target,"0.02",3)
    if err!=nil { t.Fatal(err) }
    if got.Status!="MATCHED_TOLERANCE" { t.Fatalf("status=%s score=%d",got.Status,got.Score) }
}

func TestMissingDatesDoNotPenalizeMatch(t *testing.T) {
    source:=Record{ID:"doc1",Supplier:"ABC",Reference:"INV-1",Currency:"PHP",Amount:"100.00"}
    target:=Record{ID:"erp1",Supplier:"ABC",Reference:"INV1",Currency:"PHP",Amount:"100.00"}
    got,err:=Match(source,target,"0.01",3)
    if err!=nil { t.Fatal(err) }
    if got.Status!="MATCHED_EXACT" { t.Fatalf("got %#v",got) }
    if got.Score!=100 { t.Fatalf("score=%d",got.Score) }
}
