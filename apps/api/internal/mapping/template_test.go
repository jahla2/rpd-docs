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


func TestValidateTemplateConfigAllowsSafeSplit(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Invoice No"],
          "target":"invoice_number",
          "type":"identifier",
          "operations":[
            {"operation":"split","config":{"delimiter":"-","index":1}}
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }
    split:=config.Columns[0].Operations[0]
    if split.Operation!="split" { t.Fatalf("operation=%q",split.Operation) }
    if split.Config["delimiter"]!="-" { t.Fatalf("config=%#v",split.Config) }
    if split.Config["index"]!=1 { t.Fatalf("config=%#v",split.Config) }
    if split.Config["trim"]!=true { t.Fatalf("config=%#v",split.Config) }
}

func TestValidateTemplateConfigRejectsUnsafeSplit(t *testing.T){
    cases:=[]string{
        `{"delimiter":"","index":0}`,
        `{"delimiter":"-","index":-1}`,
        `{"delimiter":"-","index":32}`,
        `{"delimiter":"-","index":1.5}`,
        `{"delimiter":"-","index":0,"regex":true}`,
        `{"delimiter":"0123456789abcdefg","index":0}`,
    }
    for _,splitConfig:=range cases {
        raw:=json.RawMessage(`{
          "columns":[
            {
              "source_aliases":["Invoice No"],
              "target":"invoice_number",
              "type":"identifier",
              "operations":[{"operation":"split","config":`+splitConfig+`}]
            }
          ]
        }`)
        if _,err:=ValidateTemplateConfig(raw); err==nil {
            t.Fatalf("expected split config to fail: %s",splitConfig)
        }
    }
}
