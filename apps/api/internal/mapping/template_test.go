package mapping

import (
    "encoding/json"
    "testing"
)

func TestValidateTemplateConfig(t *testing.T){
    raw:=json.RawMessage(`{
      "header_row":"auto",
      "columns":[
        {"source_aliases":["Inv Ref","Invoice No"],"target":"invoice_number","type":"string","required":true},
        {"source_aliases":["Qty"],"target":"quantity","type":"decimal","operations":[{"operation":"decimal"}]}
      ],
      "output":{"format":"xlsx","columns":["invoice_number","quantity"]}
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }
    if len(config.Columns)!=2 || config.Output.Format!="xlsx" { t.Fatalf("config=%#v",config) }
}

func TestRejectsArbitraryOperation(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {"source_aliases":["Qty"],"target":"quantity","type":"decimal","operations":[{"operation":"python"}]}
      ]
    }`)
    if _,err:=ValidateTemplateConfig(raw); err==nil { t.Fatal("expected validation error") }
}
