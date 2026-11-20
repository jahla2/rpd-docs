package validation

import "testing"

func TestValidateSupplierStatement(t *testing.T){
    data:=[]byte(`{
      "document_type":"supplier_statement",
      "fields":{
        "supplier_name":{"value":"ABC Trading"},
        "closing_balance":{"value":"6500.00"}
      },
      "tables":{"transactions":[{"date":"2026-11-10","balance":"6500.00"}]}
    }`)
    result,err:=ValidateDocument(data,"supplier_statement","0.01")
    if err!=nil { t.Fatal(err) }
    if !result.Valid || result.RequiresReview { t.Fatalf("unexpected validation result: %+v",result) }
}

func TestValidateBankStatementMissingAccount(t *testing.T){
    data:=[]byte(`{
      "document_type":"bank_statement",
      "fields":{"closing_balance":{"value":"28000.00"}},
      "tables":{"transactions":[]}
    }`)
    result,err:=ValidateDocument(data,"bank_statement","0.01")
    if err!=nil { t.Fatal(err) }
    if result.Valid || !result.RequiresReview { t.Fatalf("expected review: %+v",result) }
}
