package pipeline

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type JobMessage struct {
    SchemaVersion string `json:"schema_version"`
    JobID string `json:"job_id"`
    TenantID string `json:"tenant_id"`
    DocumentID string `json:"document_id"`
    RunID string `json:"run_id"`
    Step string `json:"step"`
    InputArtifact string `json:"input_artifact"`
}

type NativeResult struct {
    SchemaVersion string `json:"schema_version"`
    OutputArtifact string `json:"output_artifact"`
    QualityArtifact string `json:"quality_artifact"`
    SizeBytes int64 `json:"size_bytes"`
}

type Worker struct {
    DB *pgxpool.Pool
    Logger *slog.Logger
    StorageRoot string
    NativeEnginePath string
    AIEngineURL string
    HTTP *http.Client
}

func New(db *pgxpool.Pool, logger *slog.Logger, storageRoot,nativePath,aiURL string) *Worker {
    return &Worker{
        DB:db,Logger:logger,StorageRoot:storageRoot,NativeEnginePath:nativePath,AIEngineURL:aiURL,
        HTTP:&http.Client{Timeout:2*time.Minute},
    }
}

func (w *Worker) HandlePreprocess(ctx context.Context, job JobMessage) error {
    if job.SchemaVersion != "1.0" || job.JobID=="" || job.DocumentID=="" || job.RunID=="" || job.InputArtifact=="" {
        return fmt.Errorf("invalid job contract")
    }
    if err:=w.setStatus(ctx,job,"PREPROCESSING",false); err!=nil { return err }

    preprocessDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"preprocess")
    if err:=os.MkdirAll(preprocessDir,0o750); err!=nil { return w.fail(ctx,job,fmt.Errorf("create preprocess dir: %w",err)) }

    cmd:=exec.CommandContext(ctx,w.NativeEnginePath,"preprocess",job.InputArtifact,preprocessDir)
    out,err:=cmd.Output()
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("native preprocess: %w",err)) }

    var native NativeResult
    if err:=json.Unmarshal(out,&native); err!=nil { return w.fail(ctx,job,fmt.Errorf("decode native result: %w",err)) }
    if native.OutputArtifact=="" { return w.fail(ctx,job,fmt.Errorf("native engine returned no output artifact")) }

    if err:=w.addArtifact(ctx,job,"PREPROCESSED_PAGE",native.OutputArtifact,nil); err!=nil { return w.fail(ctx,job,err) }
    if native.QualityArtifact!="" {
        if err:=w.addArtifact(ctx,job,"QUALITY_JSON",native.QualityArtifact,nil); err!=nil { return w.fail(ctx,job,err) }
    }
    if err:=w.setStatus(ctx,job,"OCR_PROCESSING",false); err!=nil { return w.fail(ctx,job,err) }

    ocrPayload,_:=json.Marshal(map[string]any{
        "document_id":job.DocumentID,
        "page":1,
        "image_path":native.OutputArtifact,
    })
    req,err:=http.NewRequestWithContext(ctx,http.MethodPost,w.AIEngineURL+"/v1/ocr",bytes.NewReader(ocrPayload))
    if err!=nil { return w.fail(ctx,job,err) }
    req.Header.Set("Content-Type","application/json")

    resp,err:=w.HTTP.Do(req)
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("ocr request: %w",err)) }
    defer resp.Body.Close()
    if resp.StatusCode<200 || resp.StatusCode>=300 {
        return w.fail(ctx,job,fmt.Errorf("ocr returned HTTP %d",resp.StatusCode))
    }

    ocrDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"ocr")
    if err:=os.MkdirAll(ocrDir,0o750); err!=nil { return w.fail(ctx,job,err) }
    ocrPath:=filepath.Join(ocrDir,"page-001.json")
    f,err:=os.OpenFile(ocrPath,os.O_CREATE|os.O_WRONLY|os.O_TRUNC,0o640)
    if err!=nil { return w.fail(ctx,job,err) }
    _,copyErr:=f.ReadFrom(resp.Body)
    closeErr:=f.Close()
    if copyErr!=nil { return w.fail(ctx,job,copyErr) }
    if closeErr!=nil { return w.fail(ctx,job,closeErr) }

    absOCR,err:=filepath.Abs(ocrPath)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addArtifact(ctx,job,"OCR_JSON",absOCR,strPtr("application/json")); err!=nil { return w.fail(ctx,job,err) }

    if err:=w.setStatus(ctx,job,"OCR_COMPLETED",true); err!=nil { return err }
    w.Logger.Info("ocr_completed","tenant_id",job.TenantID,"document_id",job.DocumentID,"job_id",job.JobID,"ocr_artifact",absOCR)
    return nil
}

func (w *Worker) setStatus(ctx context.Context,job JobMessage,status string,completed bool) error {
    if completed {
        _,err:=w.DB.Exec(ctx,`
          UPDATE processing_jobs SET status=$1,started_at=COALESCE(started_at,now()),completed_at=now()
          WHERE id=$2 AND tenant_id=$3
        `,status,job.JobID,job.TenantID)
        if err!=nil { return err }
    } else {
        _,err:=w.DB.Exec(ctx,`
          UPDATE processing_jobs SET status=$1,started_at=COALESCE(started_at,now())
          WHERE id=$2 AND tenant_id=$3
        `,status,job.JobID,job.TenantID)
        if err!=nil { return err }
    }
    _,err:=w.DB.Exec(ctx,`
      UPDATE documents SET status=$1,updated_at=now()
      WHERE id=$2 AND tenant_id=$3
    `,status,job.DocumentID,job.TenantID)
    return err
}

func (w *Worker) fail(ctx context.Context,job JobMessage,cause error) error {
    _,_ = w.DB.Exec(ctx,`
      UPDATE processing_jobs SET status='FAILED',failed_at=now()
      WHERE id=$1 AND tenant_id=$2
    `,job.JobID,job.TenantID)
    _,_ = w.DB.Exec(ctx,`
      UPDATE documents SET status='FAILED',updated_at=now()
      WHERE id=$1 AND tenant_id=$2
    `,job.DocumentID,job.TenantID)
    return cause
}

func (w *Worker) addArtifact(ctx context.Context,job JobMessage,kind,path string,contentType *string) error {
    info,err:=os.Stat(path); if err!=nil { return err }
    _,err=w.DB.Exec(ctx,`
      INSERT INTO artifacts
        (tenant_id,document_id,run_id,artifact_type,storage_uri,content_type,size_bytes)
      VALUES ($1,$2,$3,$4,$5,$6,$7)
    `,job.TenantID,job.DocumentID,job.RunID,kind,path,contentType,info.Size())
    return err
}
func strPtr(v string)*string{return &v}
