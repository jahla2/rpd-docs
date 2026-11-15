package objectstore

import (
    "context"
    "fmt"
    "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
    Backend string
    Endpoint string
    Region string
    Bucket string
    AccessKey string
    SecretKey string
}

type Client struct {
    minio *minio.Client
    bucket string
}

func New(cfg Config)(*Client,error){
    if strings.EqualFold(strings.TrimSpace(cfg.Backend),"local") || strings.TrimSpace(cfg.Backend)=="" {
        return nil,nil
    }
    if !strings.EqualFold(cfg.Backend,"s3") { return nil,fmt.Errorf("unsupported storage backend %q",cfg.Backend) }
    if cfg.Bucket=="" { return nil,fmt.Errorf("S3 bucket is required") }

    endpoint:=strings.TrimSpace(cfg.Endpoint)
    secure:=true
    if endpoint=="" { endpoint="s3.amazonaws.com" }
    if parsed,err:=url.Parse(endpoint); err==nil && parsed.Host!="" {
        secure=parsed.Scheme!="http"
        endpoint=parsed.Host
    }
    client,err:=minio.New(endpoint,&minio.Options{
        Creds:credentials.NewStaticV4(cfg.AccessKey,cfg.SecretKey,""),
        Secure:secure,Region:cfg.Region,
    })
    if err!=nil { return nil,err }
    return &Client{minio:client,bucket:cfg.Bucket},nil
}

func (c *Client) Materialize(ctx context.Context,uri,destination string) error {
    if c==nil { return fmt.Errorf("object store is disabled") }
    bucket,key,err:=parseS3URI(uri)
    if err!=nil { return err }
    if err:=os.MkdirAll(filepath.Dir(destination),0o750); err!=nil { return err }

    downloadCtx,cancel:=context.WithTimeout(ctx,3*time.Minute)
    defer cancel()
    return c.minio.FGetObject(downloadCtx,bucket,key,destination,minio.GetObjectOptions{})
}

func (c *Client) ArchiveFile(ctx context.Context,localPath,key,contentType string)(string,error){
    if c==nil { return localPath,nil }
    key=filepath.ToSlash(strings.TrimPrefix(key,"/"))
    uploadCtx,cancel:=context.WithTimeout(ctx,3*time.Minute)
    defer cancel()
    _,err:=c.minio.FPutObject(uploadCtx,c.bucket,key,localPath,minio.PutObjectOptions{ContentType:contentType})
    if err!=nil { return "",err }
    return "s3://"+c.bucket+"/"+key,nil
}

func parseS3URI(uri string)(string,string,error){
    parsed,err:=url.Parse(uri)
    if err!=nil || parsed.Scheme!="s3" || parsed.Host=="" { return "","",fmt.Errorf("invalid S3 URI") }
    key:=strings.TrimPrefix(parsed.Path,"/")
    if key=="" { return "","",fmt.Errorf("invalid S3 key") }
    return parsed.Host,key,nil
}
