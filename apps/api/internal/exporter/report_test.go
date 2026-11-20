package exporter

import (
    "testing"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

func sampleReport() Report {
    return Report{
        Run:store.ReconciliationRun{ID:"run-1",Status:"COMPLETED_WITH_EXCEPTIONS"},
        Matches:[]store.ReconciliationMatch{{
            ID:"m1",SourceRecordID:"doc1",TargetRecordID:"erp1",
            Status:"MATCHED_EXACT",Score:"100.00",MatchingRule:"supplier_reference_amount_currency",
        }},
        Exceptions:[]store.ReconciliationException{{
            ID:"e1",SourceRecordID:"doc2",ExceptionType:"UNMATCHED",Severity:"WARNING",Status:"OPEN",
        }},
    }
}

func TestJSONExport(t *testing.T){
    data,contentType,ext,err:=Build("json",sampleReport())
    if err!=nil { t.Fatal(err) }
    if len(data)==0 || contentType!="application/json" || ext!="json" { t.Fatalf("invalid JSON export") }
}

func TestCSVExport(t *testing.T){
    data,contentType,ext,err:=Build("csv",sampleReport())
    if err!=nil { t.Fatal(err) }
    if len(data)==0 || contentType!="text/csv" || ext!="csv" { t.Fatalf("invalid CSV export") }
}

func TestXLSXExport(t *testing.T){
    data,contentType,ext,err:=Build("xlsx",sampleReport())
    if err!=nil { t.Fatal(err) }
    if len(data)<100 || contentType=="" || ext!="xlsx" { t.Fatalf("invalid XLSX export") }
}
