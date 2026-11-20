package erp

import (
    "strings"
    "testing"
)

func TestCSVImportAndHeaderMapping(t *testing.T){
    input:=`Vendor,Invoice No,Posting Date,Currency,Gross Amount
ABC Trading,INV-001,2026-11-16,PHP,"1,250.50"
XYZ Supply,INV-002,2026-11-17,PHP,900.00
`
    result,err:=Parse("erp.csv",strings.NewReader(input),nil)
    if err!=nil { t.Fatal(err) }
    if len(result.Errors)!=0 { t.Fatalf("unexpected errors: %#v",result.Errors) }
    if len(result.Rows)!=2 { t.Fatalf("rows=%d",len(result.Rows)) }
    if result.Rows[0].NormalizedSupplier!="ABCTRADING" { t.Fatalf("supplier=%q",result.Rows[0].NormalizedSupplier) }
    if result.Rows[0].NormalizedReference!="INV001" { t.Fatalf("reference=%q",result.Rows[0].NormalizedReference) }
    if result.Rows[0].Amount!="1250.5000" { t.Fatalf("amount=%q",result.Rows[0].Amount) }
}

func TestImportRejectsMissingReference(t *testing.T){
    input:=`Vendor,Amount
ABC,100
`
    result,err:=Parse("erp.csv",strings.NewReader(input),nil)
    if err!=nil { t.Fatal(err) }
    if len(result.Rows)!=0 || len(result.Errors)!=1 { t.Fatalf("result=%#v",result) }
    if result.Errors[0].Code!="MISSING_REFERENCE" { t.Fatalf("error=%#v",result.Errors[0]) }
}

func TestMappingOverride(t *testing.T){
    got:=DetectMapping([]string{"RefNum","VendorText","Net"},map[string]string{
        "RefNum":"reference",
        "VendorText":"supplier_name",
        "Net":"amount",
    })
    if got["refnum"]!="reference" || got["vendortext"]!="supplier_name" || got["net"]!="amount" {
        t.Fatalf("mapping=%#v",got)
    }
}
