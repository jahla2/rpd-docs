package storage

import (
    "bytes"
    "os"
    "testing"
)

func TestS3Integration(t *testing.T){
    endpoint:=os.Getenv("S3_TEST_ENDPOINT")
    if endpoint=="" { t.Skip("S3_TEST_ENDPOINT is not set") }

    manager,err:=NewManager(Config{
        Backend:"s3",
        Root:t.TempDir(),
        Endpoint:endpoint,
        Region:"us-east-1",
        Bucket:"rpd-ci",
        AccessKey:"minioadmin",
        SecretKey:"minioadmin",
        CreateBucket:true,
    })
    if err!=nil { t.Fatal(err) }
    if err:=manager.Ready(); err!=nil { t.Fatal(err) }

    saved,err:=manager.SaveOriginal(
        "00000000-0000-0000-0000-000000000001",
        "11111111-1111-1111-1111-111111111111",
        "sample.pdf",
        bytes.NewReader([]byte("%PDF-1.4 synthetic test")),
    )
    if err!=nil { t.Fatal(err) }
    if saved.StorageURI==saved.Path { t.Fatalf("expected S3 storage URI, got %q",saved.StorageURI) }

    data,err:=manager.Read(saved.StorageURI)
    if err!=nil { t.Fatal(err) }
    if string(data)!="%PDF-1.4 synthetic test" { t.Fatalf("data=%q",string(data)) }

    signed,err:=manager.PresignGet(saved.StorageURI,0)
    if err!=nil { t.Fatal(err) }
    if signed=="" { t.Fatal("expected signed URL") }
}
