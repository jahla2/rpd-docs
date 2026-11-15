package storage

import (
    "context"
    "fmt"
    "io"
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
    Root string
    Endpoint string
    Region string
    Bucket string
    AccessKey string
    SecretKey string
    CreateBucket bool
}

type Manager struct {
    Root string
    local *Local
    backend string
    s3 *minio.Client
    bucket string
    region string
    createBucket bool
}

func NewManager(cfg Config)(*Manager,error){
    if cfg.Root=="" { cfg.Root="./storage" }
    m:=&Manager{Root:cfg.Root,local:NewLocal(cfg.Root),backend:strings.ToLower(strings.TrimSpace(cfg.Backend))}
    if m.backend=="" || m.backend=="local" {
        m.backend="local"
        return m,nil
    }
    if m.backend!="s3" { return nil,fmt.Errorf("unsupported storage backend %q",cfg.Backend) }
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
        Secure:secure,
        Region:cfg.Region,
    })
    if err!=nil { return nil,err }
    m.s3=client
    m.bucket=cfg.Bucket
    m.region=cfg.Region
    m.createBucket=cfg.CreateBucket
    return m,nil
}

func (m *Manager) Ready() error {
    if err:=m.local.Ready(); err!=nil { return err }
    if m.backend!="s3" { return nil }
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
    defer cancel()
    exists,err:=m.s3.BucketExists(ctx,m.bucket)
    if err!=nil { return err }
    if exists { return nil }
    if !m.createBucket { return fmt.Errorf("S3 bucket %q does not exist",m.bucket) }
    return m.s3.MakeBucket(ctx,m.bucket,minio.MakeBucketOptions{Region:m.region})
}

func (m *Manager) SaveOriginal(tenantID,documentID,originalName string,src io.Reader)(SavedOriginal,error){
    saved,err:=m.local.SaveOriginal(tenantID,documentID,originalName,src)
    if err!=nil { return SavedOriginal{},err }
    saved.StorageURI=saved.Path
    if m.backend!="s3" { return saved,nil }

    now:=time.Now().UTC()
    ext:=safeExtension(originalName)
    key:=filepath.ToSlash(filepath.Join(
        "tenants",tenantID,"inbound",
        fmt.Sprintf("%04d",now.Year()),fmt.Sprintf("%02d",int(now.Month())),
        documentID,"original"+ext,
    ))
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute)
    defer cancel()
    _,err=m.s3.FPutObject(ctx,m.bucket,key,saved.Path,minio.PutObjectOptions{
        ContentType:contentTypeForExtension(ext),
    })
    if err!=nil { return SavedOriginal{},fmt.Errorf("upload original to S3: %w",err) }
    saved.StorageURI="s3://"+m.bucket+"/"+key
    return saved,nil
}

func (m *Manager) ArchiveFile(ctx context.Context,localPath,key,contentType string)(string,error){
    if m.backend!="s3" { return localPath,nil }
    if m.s3==nil { return "",fmt.Errorf("S3 storage is not configured") }
    key=filepath.ToSlash(strings.TrimPrefix(key,"/"))
    if contentType=="" { contentType="application/octet-stream" }
    uploadCtx,cancel:=context.WithTimeout(ctx,3*time.Minute)
    defer cancel()
    _,err:=m.s3.FPutObject(uploadCtx,m.bucket,key,localPath,minio.PutObjectOptions{ContentType:contentType})
    if err!=nil { return "",err }
    return "s3://"+m.bucket+"/"+key,nil
}

func (m *Manager) Read(uri string)([]byte,error){
    if !strings.HasPrefix(uri,"s3://") { return os.ReadFile(uri) }
    if m.s3==nil { return nil,fmt.Errorf("S3 storage is not configured") }
    bucket,key,err:=parseS3URI(uri)
    if err!=nil { return nil,err }
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute)
    defer cancel()
    object,err:=m.s3.GetObject(ctx,bucket,key,minio.GetObjectOptions{})
    if err!=nil { return nil,err }
    defer object.Close()
    return io.ReadAll(object)
}

func (m *Manager) PresignGet(uri string,expires time.Duration)(string,error){
    if !strings.HasPrefix(uri,"s3://") { return "",fmt.Errorf("artifact is not stored in S3") }
    if m.s3==nil { return "",fmt.Errorf("S3 storage is not configured") }
    bucket,key,err:=parseS3URI(uri)
    if err!=nil { return "",err }
    if expires<=0 { expires=15*time.Minute }
    ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
    defer cancel()
    signed,err:=m.s3.PresignedGetObject(ctx,bucket,key,expires,nil)
    if err!=nil { return "",err }
    return signed.String(),nil
}

func parseS3URI(uri string)(string,string,error){
    parsed,err:=url.Parse(uri)
    if err!=nil || parsed.Scheme!="s3" || parsed.Host=="" {
        return "","",fmt.Errorf("invalid S3 URI")
    }
    key:=strings.TrimPrefix(parsed.Path,"/")
    if key=="" { return "","",fmt.Errorf("invalid S3 object key") }
    return parsed.Host,key,nil
}

func contentTypeForExtension(ext string) string {
    switch ext {
    case ".pdf": return "application/pdf"
    case ".png": return "image/png"
    case ".jpg",".jpeg": return "image/jpeg"
    case ".tif",".tiff": return "image/tiff"
    default: return "application/octet-stream"
    }
}
