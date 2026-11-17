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


func TestValidateTemplateConfigAllowsSafeCombine(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["First Name"],
          "target":"supplier_name",
          "type":"string",
          "operations":[
            {
              "operation":"combine",
              "config":{"with":["Last Name"],"separator":" ","skip_empty":false}
            }
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }
    combine:=config.Columns[0].Operations[0]
    if combine.Operation!="combine" { t.Fatalf("operation=%q",combine.Operation) }
    aliases,ok:=combine.Config["with"].([]string)
    if !ok || len(aliases)!=1 || aliases[0]!="Last Name" {
        t.Fatalf("config=%#v",combine.Config)
    }
    if combine.Config["separator"]!=" " || combine.Config["skip_empty"]!=false {
        t.Fatalf("config=%#v",combine.Config)
    }
}

func TestValidateTemplateConfigRejectsUnsafeCombine(t *testing.T){
    cases:=[]string{
        `{}`,
        `{"with":[]}`,
        `{"with":["Last Name"],"separator":"0123456789abcdefg"}`,
        `{"with":["Last Name"],"skip_empty":"yes"}`,
        `{"with":["Last Name"],"expression":"exec()"}`,
        `{"with":[1]}`,
    }
    for _,combineConfig:=range cases {
        raw:=json.RawMessage(`{
          "columns":[
            {
              "source_aliases":["First Name"],
              "target":"supplier_name",
              "type":"string",
              "operations":[{"operation":"combine","config":`+combineConfig+`}]
            }
          ]
        }`)
        if _,err:=ValidateTemplateConfig(raw); err==nil {
            t.Fatalf("expected combine config to fail: %s",combineConfig)
        }
    }
}

func TestValidateTemplateConfigRejectsCombinePrimarySourceReuse(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["First Name"],
          "target":"supplier_name",
          "type":"string",
          "operations":[
            {"operation":"combine","config":{"with":["First Name"]}}
          ]
        }
      ]
    }`)
    if _,err:=ValidateTemplateConfig(raw); err==nil {
        t.Fatal("expected primary source reuse to fail")
    }
}


func TestValidateTemplateConfigAllowsSafeCompute(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Quantity"],
          "target":"calculated_line_total",
          "type":"decimal",
          "operations":[
            {
              "operation":"compute",
              "config":{"operator":"multiply","operand":"Unit Price","scale":2}
            }
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }
    compute:=config.Columns[0].Operations[0]
    if compute.Operation!="compute" { t.Fatalf("operation=%q",compute.Operation) }
    if compute.Config["operator"]!="multiply" ||
       compute.Config["operand"]!="Unit Price" ||
       compute.Config["scale"]!=2 {
        t.Fatalf("config=%#v",compute.Config)
    }
}

func TestValidateTemplateConfigRejectsUnsafeCompute(t *testing.T){
    cases:=[]string{
        `{}`,
        `{"operator":"eval","operand":"Unit Price","scale":2}`,
        `{"operator":"multiply","operand":"","scale":2}`,
        `{"operator":"multiply","operand":"Unit Price"}`,
        `{"operator":"multiply","operand":"Unit Price","scale":9}`,
        `{"operator":"multiply","operand":"Unit Price","scale":2,"script":"x"}`,
    }
    for _,computeConfig:=range cases {
        raw:=json.RawMessage(`{
          "columns":[
            {
              "source_aliases":["Quantity"],
              "target":"calculated_line_total",
              "type":"decimal",
              "operations":[{"operation":"compute","config":`+computeConfig+`}]
            }
          ]
        }`)
        if _,err:=ValidateTemplateConfig(raw); err==nil {
            t.Fatalf("expected compute config to fail: %s",computeConfig)
        }
    }
}

func TestValidateTemplateConfigRejectsComputeOnNonDecimalTarget(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Quantity"],
          "target":"description",
          "type":"string",
          "operations":[
            {"operation":"compute","config":{"operator":"multiply","operand":"Unit Price","scale":2}}
          ]
        }
      ]
    }`)
    if _,err:=ValidateTemplateConfig(raw); err==nil {
        t.Fatal("expected compute on non-decimal target to fail")
    }
}

func TestValidateTemplateConfigRejectsComputePrimaryOperandReuse(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Quantity"],
          "target":"calculated_line_total",
          "type":"decimal",
          "operations":[
            {"operation":"compute","config":{"operator":"multiply","operand":"Quantity","scale":2}}
          ]
        }
      ]
    }`)
    if _,err:=ValidateTemplateConfig(raw); err==nil {
        t.Fatal("expected primary operand reuse to fail")
    }
}
