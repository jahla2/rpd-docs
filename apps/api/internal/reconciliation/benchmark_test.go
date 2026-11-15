package reconciliation

import (
    "testing"
    "time"
)

func BenchmarkMatch(b *testing.B){
    day:=time.Date(2026,11,16,0,0,0,0,time.UTC)
    source:=Record{ID:"doc",Supplier:"ABC Trading",Reference:"INV-001",Currency:"PHP",Amount:"1250.00",Date:day}
    target:=Record{ID:"erp",Supplier:"ABC Trading",Reference:"INV001",Currency:"PHP",Amount:"1250.00",Date:day}
    b.ResetTimer()
    for i:=0;i<b.N;i++ {
        _,_ = MatchAdvanced(source,target,"0.01",3,1.0)
    }
}

func BenchmarkFindAmountCombination(b *testing.B){
    entries:=[]AmountEntry{
        {ID:"1",Amount:"500.00"},{ID:"2",Amount:"250.00"},{ID:"3",Amount:"125.00"},
        {ID:"4",Amount:"75.00"},{ID:"5",Amount:"50.00"},{ID:"6",Amount:"25.00"},
        {ID:"7",Amount:"10.00"},{ID:"8",Amount:"5.00"},
    }
    b.ResetTimer()
    for i:=0;i<b.N;i++ {
        _,_,_ = FindAmountCombination(entries,"1000.00","0.01",4)
    }
}
