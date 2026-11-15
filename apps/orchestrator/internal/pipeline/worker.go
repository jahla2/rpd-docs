package pipeline

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
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

type OCRBlock struct {
    BlockID string `json:"block_id"`
    Text string `json:"text"`
    BBox []float64 `json:"bbox"`
    Confidence float64 `json:"confidence"`
}

type OCRResponse struct {
    SchemaVersion string `json:"schema_version"`
    DocumentID string `json:"document_id"`
    Page int `json:"page"`
    Engine string `json:"engine"`
    Blocks []OCRBlock `json:"blocks"`
}

type TableResponse struct {
    SchemaVersion string `json:"schema_version"`
    DocumentID string `json:"document_id"`
    Page int `json:"page"`
    Model string `json:"model"`
    PredHTML *string `json:"pred_html"`
    CellBBoxes any `json:"cell_bboxes"`
    LogicPoints any `json:"logic_points"`
    Elapsed *float64 `json:"elapsed"`
}

type CanonicalResponse struct {
    SchemaVersion string `json:"schema_version"`
    DocumentID string `json:"document_id"`
    DocumentType string `json:"document_type"`
    RequiresReview bool `json:"requires_review"`
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
        HTTP:&http.Client{Timeout:3*time.Minute},
    }
}

func (w *Worker) HandlePreprocess(ctx context.Context, job JobMessage) error {
    if job.SchemaVersion!="1.0" || job.JobID=="" || job.TenantID=="" || job.DocumentID=="" || job.RunID=="" || job.InputArtifact=="" {
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
        if err:=w.addArtifact(ctx,job,"QUALITY_JSON",native.QualityArtifact,strPtr("application/json")); err!=nil { return w.fail(ctx,job,err) }
    }

    // OCR
    if err:=w.setStatus(ctx,job,"OCR_PROCESSING",false); err!=nil { return w.fail(ctx,job,err) }
    ocrBody,err:=w.postJSON(ctx,"/v1/ocr",map[string]any{
        "document_id":job.DocumentID,"page":1,"image_path":native.OutputArtifact,
    })
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("ocr: %w",err)) }

    var ocr OCRResponse
    if err:=json.Unmarshal(ocrBody,&ocr); err!=nil { return w.fail(ctx,job,fmt.Errorf("decode OCR result: %w",err)) }
    if ocr.SchemaVersion!="1.0" { return w.fail(ctx,job,fmt.Errorf("unsupported OCR schema version %q",ocr.SchemaVersion)) }

    ocrDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"ocr")
    ocrPath,err:=writeArtifactFile(ocrDir,"page-001.json",ocrBody)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addArtifact(ctx,job,"OCR_JSON",ocrPath,strPtr("application/json")); err!=nil { return w.fail(ctx,job,err) }
    if err:=w.setStatus(ctx,job,"OCR_COMPLETED",false); err!=nil { return w.fail(ctx,job,err) }

    // Table structure. MVP uses the full preprocessed page. A dedicated
    // table-region detector can later crop and dispatch multiple regions.
    if err:=w.setStatus(ctx,job,"TABLE_PROCESSING",false); err!=nil { return w.fail(ctx,job,err) }
    tableBody,err:=w.postJSON(ctx,"/v1/tables",map[string]any{
        "document_id":job.DocumentID,
        "page":1,
        "image_path":native.OutputArtifact,
        "ocr_blocks":ocr.Blocks,
    })
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("table recognition: %w",err)) }

    var table TableResponse
    if err:=json.Unmarshal(tableBody,&table); err!=nil { return w.fail(ctx,job,fmt.Errorf("decode table result: %w",err)) }
    tableDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"tables")
    tablePath,err:=writeArtifactFile(tableDir,"table-001.json",tableBody)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addArtifact(ctx,job,"TABLE_JSON",tablePath,strPtr("application/json")); err!=nil { return w.fail(ctx,job,err) }
    if table.PredHTML!=nil && *table.PredHTML!="" {
        htmlPath,htmlErr:=writeArtifactFile(tableDir,"table-001.html",[]byte(*table.PredHTML))
        if htmlErr!=nil { return w.fail(ctx,job,htmlErr) }
        if err:=w.addArtifact(ctx,job,"TABLE_HTML",htmlPath,strPtr("text/html")); err!=nil { return w.fail(ctx,job,err) }
    }
    if err:=w.setStatus(ctx,job,"TABLES_COMPLETED",false); err!=nil { return w.fail(ctx,job,err) }

    // Deterministic invoice extraction from OCR + table structure.
    if err:=w.setStatus(ctx,job,"MAPPING",false); err!=nil { return w.fail(ctx,job,err) }
    extractPayload:=map[string]any{
        "document_id":job.DocumentID,
        "page":1,
        "ocr_blocks":ocr.Blocks,
        "table_html":table.PredHTML,
    }
    canonicalBody,err:=w.postJSON(ctx,"/v1/extract/invoice",extractPayload)
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("invoice extraction: %w",err)) }

    var canonical CanonicalResponse
    if err:=json.Unmarshal(canonicalBody,&canonical); err!=nil { return w.fail(ctx,job,fmt.Errorf("decode canonical result: %w",err)) }
    if canonical.DocumentType=="" { canonical.DocumentType="invoice" }

    if err:=w.setStatus(ctx,job,"VALIDATING",false); err!=nil { return w.fail(ctx,job,err) }
    versionID,versionNumber,err:=w.createDocumentVersion(ctx,job,canonical.DocumentType)
    if err!=nil { return w.fail(ctx,job,err) }

    finalDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"documents",job.DocumentID,versionID)
    canonicalPath,err:=writeArtifactFile(finalDir,"canonical.json",canonicalBody)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addVersionArtifact(ctx,job,versionID,"CANONICAL_JSON",canonicalPath,strPtr("application/json")); err!=nil {
        return w.fail(ctx,job,err)
    }

    finalStatus:="VALIDATED"
    if canonical.RequiresReview { finalStatus="REVIEW_REQUIRED" }
    if err:=w.setStatus(ctx,job,finalStatus,true); err!=nil { return err }

    w.Logger.Info("document_pipeline_completed",
        "tenant_id",job.TenantID,"document_id",job.DocumentID,"job_id",job.JobID,
        "document_version_id",versionID,"version_number",versionNumber,
        "status",finalStatus,"canonical_artifact",canonicalPath)
    return nil
}

func (w *Worker) postJSON(ctx context.Context,path string,payload any) ([]byte,error) {
    body,err:=json.Marshal(payload); if err!=nil { return nil,err }
    req,err:=http.NewRequestWithContext(ctx,http.MethodPost,w.AIEngineURL+path,bytes.NewReader(body))
    if err!=nil { return nil,err }
    req.Header.Set("Content-Type","application/json")
    resp,err:=w.HTTP.Do(req)
    if err!=nil { return nil,err }
    defer resp.Body.Close()
    responseBody,readErr:=io.ReadAll(io.LimitReader(resp.Body,32<<20))
    if readErr!=nil { return nil,readErr }
    if resp.StatusCode<200 || resp.StatusCode>=300 {
        return nil,fmt.Errorf("HTTP %d: %s",resp.StatusCode,string(responseBody))
    }
    return responseBody,nil
}

func writeArtifactFile(dir,name string,data []byte)(string,error){
    if err:=os.MkdirAll(dir,0o750); err!=nil { return "",err }
    path:=filepath.Join(dir,name)
    if err:=os.WriteFile(path,data,0o640); err!=nil { return "",err }
    return filepath.Abs(path)
}

func (w *Worker) createDocumentVersion(ctx context.Context,job JobMessage,documentType string)(string,int,error){
    var id string
    var version int
    err:=w.DB.QueryRow(ctx,`
      INSERT INTO document_versions
        (tenant_id,document_id,version_number,pipeline_version)
      SELECT $1,$2,COALESCE(MAX(version_number),0)+1,'mvp-0.2'
      FROM document_versions
      WHERE tenant_id=$1 AND document_id=$2
      RETURNING id::text,version_number
    `,job.TenantID,job.DocumentID).Scan(&id,&version)
    if err!=nil { return "",0,err }
    _,err=w.DB.Exec(ctx,`
      UPDATE documents SET document_type=$1,updated_at=now()
      WHERE id=$2 AND tenant_id=$3
    `,documentType,job.DocumentID,job.TenantID)
    return id,version,err
}

func (w *Worker) setStatus(ctx context.Context,job JobMessage,status string,completed bool) error {
    if completed {
        _,err:=w.DB.Exec(ctx,`
          UPDATE processing_jobs
          SET status=$1,started_at=COALESCE(started_at,now()),completed_at=now()
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

func (w *Worker) addVersionArtifact(ctx context.Context,job JobMessage,versionID,kind,path string,contentType *string) error {
    info,err:=os.Stat(path); if err!=nil { return err }
    _,err=w.DB.Exec(ctx,`
      INSERT INTO artifacts
        (tenant_id,document_id,document_version_id,run_id,artifact_type,storage_uri,content_type,size_bytes)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
    `,job.TenantID,job.DocumentID,versionID,job.RunID,kind,path,contentType,info.Size())
    return err
}
func strPtr(v string)*string{return &v}
