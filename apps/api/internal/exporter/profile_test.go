package exporter

import (
    "strings"
    "testing"
)

func TestProfileCSVColumnOrderAndHeaders(t *testing.T){
    data,contentType,ext,err:=BuildWithProfile(Profile{
        Format:"csv",
        Columns:[]string{"status","source_record_id","score"},
        HeaderAliases:map[string]string{"source_record_id":"Invoice ID"},
    },sampleReport())
    if err!=nil { t.Fatal(err) }
    if contentType!="text/csv" || ext!="csv" { t.Fatalf("%s %s",contentType,ext) }
    first:=strings.Split(string(data),"\n")[0]
    if first!="status,Invoice ID,score" { t.Fatalf("header=%q",first) }
}

func TestProfileRejectsUnknownColumn(t *testing.T){
    if err:=ValidateProfile("csv",[]string{"password"},nil); err==nil { t.Fatal("expected validation error") }
}
