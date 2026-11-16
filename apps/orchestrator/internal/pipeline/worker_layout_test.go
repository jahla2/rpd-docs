package pipeline

import (
    "encoding/json"
    "testing"
)

func TestCanonicalLayoutConfigBuildsNormalizedFieldRegions(t *testing.T){
    body:=[]byte(`{
      "document_type":"supplier_statement",
      "fields":{
        "supplier_name":{
          "raw":"Supplier: ABC Trading",
          "value":"ABC Trading",
          "data_type":"string",
          "evidence":{
            "page":1,
            "bbox_normalized":[0.10,0.10,0.30,0.13]
          }
        },
        "closing_balance":{
          "raw":"Current Balance: 6500.00",
          "value":"6500.00",
          "data_type":"decimal",
          "evidence":{
            "page":1,
            "bbox_normalized":[0.65,0.15,0.85,0.18]
          }
        }
      }
    }`)

    supplier,config,err:=canonicalLayoutConfig(body)
    if err!=nil { t.Fatal(err) }
    if supplier!="abc trading" { t.Fatalf("unexpected supplier key %q",supplier) }

    regions,ok:=config["field_regions"].(map[string]any)
    if !ok { t.Fatalf("field_regions missing: %#v",config) }
    closing,ok:=regions["closing_balance"].(map[string]any)
    if !ok { t.Fatalf("closing balance region missing: %#v",regions) }
    if closing["raw_prefix"]!="Current Balance" {
        t.Fatalf("unexpected prefix %#v",closing["raw_prefix"])
    }

    encoded,err:=json.Marshal(config)
    if err!=nil || len(encoded)==0 { t.Fatalf("config should be JSON serializable: %v",err) }
}

func TestNormalizeLayoutKey(t *testing.T){
    if got:=normalizeLayoutKey("  ABC Trading, Inc. "); got!="abc trading inc" {
        t.Fatalf("unexpected normalized layout key %q",got)
    }
}
