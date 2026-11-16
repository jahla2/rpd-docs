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
