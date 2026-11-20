package storage

import "testing"

func TestParseS3URI(t *testing.T){
    bucket,key,err:=parseS3URI("s3://my-bucket/tenants/t1/file.pdf")
    if err!=nil { t.Fatal(err) }
    if bucket!="my-bucket" || key!="tenants/t1/file.pdf" { t.Fatalf("%s %s",bucket,key) }
}

func TestParseS3URIRejectsInvalid(t *testing.T){
    if _,_,err:=parseS3URI("/tmp/file.pdf"); err==nil { t.Fatal("expected error") }
}
