package mapping

import (
    "encoding/json"
    "testing"
)

func TestPreviewTemplateUsesSavedAliasesAndTransforms(t *testing.T) {
    raw := json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Inv Ref"],
          "target":"invoice_number",
          "type":"identifier",
          "required":true,
          "operations":[{"operation":"normalize","config":{"case":"upper"}}]
        },
        {
          "source_aliases":["Invoice Date"],
          "target":"invoice_date",
          "type":"date",
          "required":true,
          "operations":[{"operation":"date"}]
        },
        {
          "source_aliases":["Total"],
          "target":"line_total",
          "type":"decimal",
          "operations":[{"operation":"decimal"}]
        }
      ]
    }`)
    config, err := ValidateTemplateConfig(raw)
    if err != nil {
        t.Fatal(err)
    }

    preview := PreviewTemplate(
        config,
        []string{"Inv Ref", "Invoice Date", "Total"},
        map[string][]string{
            "Inv Ref": {" inv-001 "},
            "Invoice Date": {"16/11/2026"},
            "Total": {"₱ 1,250.50"},
        },
    )

    if len(preview.ValidationErrors) != 0 {
        t.Fatalf("validation errors=%v", preview.ValidationErrors)
    }
    if preview.Confidence != 1 {
        t.Fatalf("confidence=%v", preview.Confidence)
    }
    if len(preview.SampleTransformedRows) != 1 {
        t.Fatalf("rows=%v", preview.SampleTransformedRows)
    }
    row := preview.SampleTransformedRows[0]
    if row["invoice_number"] != "INV-001" {
        t.Fatalf("invoice_number=%q", row["invoice_number"])
    }
    if row["invoice_date"] != "2026-11-16" {
        t.Fatalf("invoice_date=%q", row["invoice_date"])
    }
    if row["line_total"] != "1250.50" {
        t.Fatalf("line_total=%q", row["line_total"])
    }
}

func TestPreviewTemplateReportsMissingRequiredMappings(t *testing.T) {
    raw := json.RawMessage(`{
      "columns":[
        {"source_aliases":["Invoice No"],"target":"invoice_number","type":"identifier","required":true}
      ]
    }`)
    config, err := ValidateTemplateConfig(raw)
    if err != nil {
        t.Fatal(err)
    }

    preview := PreviewTemplate(config, []string{"Other"}, map[string][]string{})
    if len(preview.ValidationErrors) != 1 {
        t.Fatalf("validation errors=%v", preview.ValidationErrors)
    }
    if preview.Confidence != 0 {
        t.Fatalf("confidence=%v", preview.Confidence)
    }
}


func TestPreviewTemplateAppliesSplit(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Invoice No"],
          "target":"invoice_number",
          "type":"identifier",
          "required":true,
          "operations":[
            {"operation":"split","config":{"delimiter":"-","index":1,"trim":true}}
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }

    preview:=PreviewTemplate(
        config,
        []string{"Invoice No"},
        map[string][]string{"Invoice No":{"INV- 001 "}},
    )
    if len(preview.ValidationErrors)!=0 { t.Fatalf("errors=%v",preview.ValidationErrors) }
    if len(preview.SampleTransformedRows)!=1 { t.Fatalf("rows=%v",preview.SampleTransformedRows) }
    if got:=preview.SampleTransformedRows[0]["invoice_number"]; got!="001" {
        t.Fatalf("invoice_number=%q",got)
    }
}

func TestApplyOperationsSplitMissingPartFailsClosed(t *testing.T){
    value,excluded:=ApplyOperations("INV-001",[]Transform{{
        Operation:"split",
        Config:map[string]any{"delimiter":"-","index":2,"trim":true},
    }})
    if excluded { t.Fatal("split should not mark excluded") }
    if value!=nil { t.Fatalf("value=%q",*value) }
}


func TestPreviewTemplateSplitFeedsMultipleTargets(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Composite Ref"],
          "target":"supplier_code",
          "type":"identifier",
          "required":true,
          "operations":[
            {"operation":"split","config":{"delimiter":"-","index":0}}
          ]
        },
        {
          "source_aliases":["Composite Ref"],
          "target":"invoice_number",
          "type":"identifier",
          "required":true,
          "operations":[
            {"operation":"split","config":{"delimiter":"-","index":1}}
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }

    preview:=PreviewTemplate(
        config,
        []string{"Composite Ref"},
        map[string][]string{"Composite Ref":{"SUP001-INV100"}},
    )
    if len(preview.ValidationErrors)!=0 { t.Fatalf("errors=%v",preview.ValidationErrors) }
    if preview.Confidence!=1 { t.Fatalf("confidence=%v",preview.Confidence) }
    row:=preview.SampleTransformedRows[0]
    if row["supplier_code"]!="SUP001" || row["invoice_number"]!="INV100" {
        t.Fatalf("row=%v",row)
    }
    if len(preview.Suggestions)!=1 || len(preview.Suggestions[0].Targets)!=2 {
        t.Fatalf("suggestions=%v",preview.Suggestions)
    }
}


func TestPreviewTemplateAppliesCombine(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["First Name"],
          "target":"supplier_name",
          "type":"string",
          "required":true,
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

    preview:=PreviewTemplate(
        config,
        []string{"First Name","Last Name"},
        map[string][]string{
            "First Name":{"ABC"},
            "Last Name":{"Trading"},
        },
    )
    if len(preview.ValidationErrors)!=0 { t.Fatalf("errors=%v",preview.ValidationErrors) }
    if got:=preview.SampleTransformedRows[0]["supplier_name"]; got!="ABC Trading" {
        t.Fatalf("supplier_name=%q",got)
    }
}

func TestPreviewTemplateCombineMissingSourceFailsClosed(t *testing.T){
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

    preview:=PreviewTemplate(
        config,
        []string{"First Name"},
        map[string][]string{"First Name":{"ABC"}},
    )
    if len(preview.ValidationErrors)==0 {
        t.Fatalf("expected missing combine source error: %#v",preview)
    }
    if got:=preview.SampleTransformedRows[0]["supplier_name"]; got!="" {
        t.Fatalf("expected fail-closed combine output, got %q",got)
    }
}

func TestPreviewTemplateCombineSkipEmptyAllowsOptionalSource(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["First Name"],
          "target":"supplier_name",
          "type":"string",
          "operations":[
            {
              "operation":"combine",
              "config":{"with":["Middle Name"],"separator":" ","skip_empty":true}
            }
          ]
        }
      ]
    }`)
    config,err:=ValidateTemplateConfig(raw)
    if err!=nil { t.Fatal(err) }

    preview:=PreviewTemplate(
        config,
        []string{"First Name"},
        map[string][]string{"First Name":{"ABC"}},
    )
    if len(preview.ValidationErrors)!=0 { t.Fatalf("errors=%v",preview.ValidationErrors) }
    if got:=preview.SampleTransformedRows[0]["supplier_name"]; got!="ABC" {
        t.Fatalf("supplier_name=%q",got)
    }
}


func TestPreviewTemplateAppliesCompute(t *testing.T){
    raw:=json.RawMessage(`{
      "columns":[
        {
          "source_aliases":["Quantity"],
          "target":"calculated_line_total",
          "type":"decimal",
          "required":true,
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

    preview:=PreviewTemplate(
        config,
        []string{"Quantity","Unit Price"},
        map[string][]string{
            "Quantity":{"2"},
            "Unit Price":{"500.00"},
        },
    )
    if len(preview.ValidationErrors)!=0 { t.Fatalf("errors=%v",preview.ValidationErrors) }
    if got:=preview.SampleTransformedRows[0]["calculated_line_total"]; got!="1000.00" {
        t.Fatalf("calculated_line_total=%q",got)
    }
}

func TestPreviewTemplateComputeMissingOperandFailsClosed(t *testing.T){
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

    preview:=PreviewTemplate(
        config,
        []string{"Quantity"},
        map[string][]string{"Quantity":{"2"}},
    )
    if len(preview.ValidationErrors)==0 {
        t.Fatalf("expected missing compute operand error: %#v",preview)
    }
    if got:=preview.SampleTransformedRows[0]["calculated_line_total"]; got!="" {
        t.Fatalf("expected fail-closed compute output, got %q",got)
    }
}

func TestApplyOperationsComputeDivideByZeroFailsClosed(t *testing.T){
    value,excluded:=ApplyOperationsWithContext(
        "10",
        []Transform{{
            Operation:"compute",
            Config:map[string]any{
                "operator":"divide",
                "operand":"Divisor",
                "scale":2,
            },
        }},
        map[string]string{"divisor":"0"},
    )
    if excluded { t.Fatal("compute should not mark excluded") }
    if value!=nil { t.Fatalf("value=%q",*value) }
}
