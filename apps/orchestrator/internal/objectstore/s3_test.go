package objectstore

import "testing"

func TestParseS3URI(t *testing.T){
    bucket,key,err:=parseS3URI("s3://bucket/path/to/file.json")
    if err!=nil { t.Fatal(err) }
    if bucket!="bucket" || key!="path/to/file.json" { t.Fatalf("%s %s",bucket,key) }
}
