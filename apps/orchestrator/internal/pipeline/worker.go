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
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jahla2/rpd-docs/apps/orchestrator/internal/objectstore"
    "github.com/jahla2/rpd-docs/apps/orchestrator/internal/validation"
)

type JobMessage struct {
    SchemaVersion string `json:"schema_version"`
    JobID string `json:"job_id"`
    TenantID string `json:"tenant_id"`
    DocumentID string `json:"document_id"`
    RunID string `json:"run_id"`
    Step string `json:"step"`
    InputArtifact string `json:"input_artifact"`
    Options map[string]any `json:"options,omitempty"`
}

type NativePrepareResult struct {
    SchemaVersion string `json:"schema_version"`
    PageCount int `json:"page_count"`
    Pages []NativePage `json:"pages"`
}

type NativePage struct {
    Page int `json:"page"`
    ImageArtifact string `json:"image_artifact"`
    QualityArtifact string `json:"quality_artifact"`
    TextLayoutArtifact *string `json:"text_layout_artifact"`
    SizeBytes int64 `json:"size_bytes"`
    Mode string `json:"mode"`
    SearchableText bool `json:"searchable_text"`
    Blank bool `json:"blank"`
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

type CanonicalEvidence struct {
    Page int `json:"page"`
    BlockIDs []string `json:"block_ids"`
    Method string `json:"method"`
    Confidence float64 `json:"confidence"`
}

type CanonicalField struct {
    Raw string `json:"raw"`
    Value string `json:"value"`
    Evidence CanonicalEvidence `json:"evidence"`
}

type CanonicalInvoiceDocument struct {
    SupplierName *CanonicalField `json:"supplier_name"`
    SupplierCode *CanonicalField `json:"supplier_code"`
    InvoiceNumber *CanonicalField `json:"invoice_number"`
    InvoiceDate *CanonicalField `json:"invoice_date"`
    DueDate *CanonicalField `json:"due_date"`
    PONumber *CanonicalField `json:"po_number"`
    Currency *CanonicalField `json:"currency"`
    Subtotal *CanonicalField `json:"subtotal"`
    Tax *CanonicalField `json:"tax"`
    GrandTotal *CanonicalField `json:"grand_total"`
    LineItems []map[string]any `json:"line_items"`
}

type pageExtraction struct {
    Page int `json:"page"`
    OCRBlocks []OCRBlock `json:"ocr_blocks"`
    TableHTML *string `json:"table_html"`
}

type Worker struct {
    DB *pgxpool.Pool
    Logger *slog.Logger
    StorageRoot string
    NativeEnginePath string
    AIEngineURL string
    HTTP *http.Client
    AgentFallbackEnabled bool
    AgentMinConfidence float64
    ObjectStore *objectstore.Client
}

func New(db *pgxpool.Pool, logger *slog.Logger, storageRoot,nativePath,aiURL string,agentEnabled bool,agentMinConfidence float64,obj *objectstore.Client) *Worker {
    if agentMinConfidence<=0 || agentMinConfidence>1 { agentMinConfidence=0.90 }
    return &Worker{
        DB:db,Logger:logger,StorageRoot:storageRoot,NativeEnginePath:nativePath,AIEngineURL:aiURL,
        HTTP:&http.Client{Timeout:5*time.Minute},
        AgentFallbackEnabled:agentEnabled,AgentMinConfidence:agentMinConfidence,ObjectStore:obj,
    }
}

func (w *Worker) HandlePreprocess(ctx context.Context, job JobMessage) error {
    if job.SchemaVersion!="1.0" || job.JobID=="" || job.TenantID=="" || job.DocumentID=="" || job.RunID=="" || job.InputArtifact=="" {
        return fmt.Errorf("invalid job contract")
    }

    completed,err:=w.beginStep(ctx,job)
    if err!=nil { return err }
    if completed {
        w.Logger.Info("job_step_already_completed","job_id",job.JobID,"run_id",job.RunID,"step",job.Step)
        return nil
    }

    if err:=w.setStatus(ctx,job,"PREPROCESSING",false,5); err!=nil { return w.fail(ctx,job,err) }

    preprocessDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"preprocess")
    if err:=os.MkdirAll(preprocessDir,0o750); err!=nil {
        return w.fail(ctx,job,fmt.Errorf("create preprocess dir: %w",err))
    }

    inputArtifact:=job.InputArtifact
    if _,statErr:=os.Stat(inputArtifact); statErr!=nil && w.ObjectStore!=nil {
        sourceURI,_:=job.Options["source_uri"].(string)
        if sourceURI=="" { return w.fail(ctx,job,fmt.Errorf("input artifact is unavailable locally and source_uri is missing")) }
        extension:=filepath.Ext(sourceURI)
        if extension=="" { extension=".bin" }
        materialized:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"source","original"+extension)
        if err:=w.ObjectStore.Materialize(ctx,sourceURI,materialized); err!=nil {
            return w.fail(ctx,job,fmt.Errorf("materialize source artifact: %w",err))
        }
        inputArtifact=materialized
    }

    cmd:=exec.CommandContext(ctx,w.NativeEnginePath,"prepare",inputArtifact,preprocessDir)
    out,err:=cmd.Output()
    if err!=nil {
        return w.fail(ctx,job,fmt.Errorf("native prepare: %w",err))
    }

    var native NativePrepareResult
    if err:=json.Unmarshal(out,&native); err!=nil {
        return w.fail(ctx,job,fmt.Errorf("decode native prepare result: %w",err))
    }
    if native.SchemaVersion!="2.0" || native.PageCount<1 || len(native.Pages)!=native.PageCount {
        return w.fail(ctx,job,fmt.Errorf("invalid native prepare contract"))
    }

    _=w.emitEvent(ctx,job,"document.pages_discovered","PREPROCESSED",10,nil,&native.PageCount,map[string]any{
        "page_count":native.PageCount,
    })

    for _,page:=range native.Pages {
        if page.ImageArtifact=="" {
            return w.fail(ctx,job,fmt.Errorf("page %d has no image artifact",page.Page))
        }
        if err:=w.addArtifact(ctx,job,"PREPROCESSED_PAGE",page.ImageArtifact,strPtr("image/png")); err!=nil {
            return w.fail(ctx,job,err)
        }
        if page.QualityArtifact!="" {
            if err:=w.addArtifact(ctx,job,"QUALITY_JSON",page.QualityArtifact,strPtr("application/json")); err!=nil {
                return w.fail(ctx,job,err)
            }
        }
        if page.TextLayoutArtifact!=nil && *page.TextLayoutArtifact!="" {
            if err:=w.addArtifact(ctx,job,"PDF_TEXT_LAYOUT",*page.TextLayoutArtifact,strPtr("text/html")); err!=nil {
                return w.fail(ctx,job,err)
            }
        }

        route:="ocr"
        if page.Blank { route="blank" } else if page.SearchableText && page.TextLayoutArtifact!=nil { route="embedded_text" }
        if err:=w.upsertPage(ctx,job,page,route,"PREPROCESSED","",""); err!=nil {
            return w.fail(ctx,job,err)
        }
    }

    if err:=w.setStatus(ctx,job,"OCR_PROCESSING",false,15); err!=nil { return w.fail(ctx,job,err) }

    extractions:=make([]pageExtraction,0,native.PageCount)
    pageOCR:=make(map[int]OCRResponse,native.PageCount)

    for index,page:=range native.Pages {
        var ocrBody []byte
        var ocr OCRResponse

        if page.Blank {
            ocr=OCRResponse{
                SchemaVersion:"1.0",DocumentID:job.DocumentID,Page:page.Page,Engine:"blank",Blocks:[]OCRBlock{},
            }
            ocrBody,_=json.Marshal(ocr)
        } else if page.SearchableText && page.TextLayoutArtifact!=nil {
            ocrBody,err=w.postJSON(ctx,"/v1/pdftext",map[string]any{
                "document_id":job.DocumentID,
                "page":page.Page,
                "layout_path":*page.TextLayoutArtifact,
                "image_path":page.ImageArtifact,
            })
            if err!=nil { return w.fail(ctx,job,fmt.Errorf("page %d embedded text: %w",page.Page,err)) }
            if err:=json.Unmarshal(ocrBody,&ocr); err!=nil {
                return w.fail(ctx,job,fmt.Errorf("decode page %d embedded text: %w",page.Page,err))
            }
        } else {
            ocrBody,err=w.postJSON(ctx,"/v1/ocr",map[string]any{
                "document_id":job.DocumentID,
                "page":page.Page,
                "image_path":page.ImageArtifact,
            })
            if err!=nil { return w.fail(ctx,job,fmt.Errorf("page %d OCR: %w",page.Page,err)) }
            if err:=json.Unmarshal(ocrBody,&ocr); err!=nil {
                return w.fail(ctx,job,fmt.Errorf("decode page %d OCR: %w",page.Page,err))
            }
        }

        if ocr.SchemaVersion!="1.0" {
            return w.fail(ctx,job,fmt.Errorf("unsupported text schema version %q on page %d",ocr.SchemaVersion,page.Page))
        }

        ocrDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"ocr")
        ocrPath,err:=writeArtifactFile(ocrDir,fmt.Sprintf("page-%03d.json",page.Page),ocrBody)
        if err!=nil { return w.fail(ctx,job,err) }
        if err:=w.addArtifact(ctx,job,"OCR_JSON",ocrPath,strPtr("application/json")); err!=nil { return w.fail(ctx,job,err) }

        pageOCR[page.Page]=ocr
        if err:=w.updatePageOutput(ctx,job,page.Page,"TEXT_COMPLETED",ocrPath,""); err!=nil {
            return w.fail(ctx,job,err)
        }

        pageNumber:=page.Page
        progress:=pageProgress(15,30,index+1,native.PageCount)
        _=w.emitEvent(ctx,job,"page.text_completed","OCR_PROCESSING",progress,&pageNumber,&native.PageCount,map[string]any{
            "engine":ocr.Engine,
            "block_count":len(ocr.Blocks),
            "route":func() string {
                if page.Blank { return "blank" }
                if page.SearchableText { return "embedded_text" }
                return "ocr"
            }(),
        })
    }

    if err:=w.setStatus(ctx,job,"OCR_COMPLETED",false,45); err!=nil { return w.fail(ctx,job,err) }
    if err:=w.setStatus(ctx,job,"TABLE_PROCESSING",false,50); err!=nil { return w.fail(ctx,job,err) }

    for index,page:=range native.Pages {
        ocr:=pageOCR[page.Page]
        var table TableResponse

        if !page.Blank && len(ocr.Blocks)>0 {
            tableBody,tableErr:=w.postJSON(ctx,"/v1/tables",map[string]any{
                "document_id":job.DocumentID,
                "page":page.Page,
                "image_path":page.ImageArtifact,
                "ocr_blocks":ocr.Blocks,
                "auto_crop":true,
            })
            if tableErr!=nil {
                return w.fail(ctx,job,fmt.Errorf("page %d table recognition: %w",page.Page,tableErr))
            }
            if err:=json.Unmarshal(tableBody,&table); err!=nil {
                return w.fail(ctx,job,fmt.Errorf("decode page %d table result: %w",page.Page,err))
            }

            tableDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"processing",job.DocumentID,job.RunID,"tables")
            tablePath,err:=writeArtifactFile(tableDir,fmt.Sprintf("table-%03d.json",page.Page),tableBody)
            if err!=nil { return w.fail(ctx,job,err) }
            if err:=w.addArtifact(ctx,job,"TABLE_JSON",tablePath,strPtr("application/json")); err!=nil {
                return w.fail(ctx,job,err)
            }

            if table.PredHTML!=nil && *table.PredHTML!="" {
                htmlPath,htmlErr:=writeArtifactFile(tableDir,fmt.Sprintf("table-%03d.html",page.Page),[]byte(*table.PredHTML))
                if htmlErr!=nil { return w.fail(ctx,job,htmlErr) }
                if err:=w.addArtifact(ctx,job,"TABLE_HTML",htmlPath,strPtr("text/html")); err!=nil {
                    return w.fail(ctx,job,err)
                }
            }
            if err:=w.updatePageOutput(ctx,job,page.Page,"TABLES_COMPLETED","",tablePath); err!=nil {
                return w.fail(ctx,job,err)
            }
        } else {
            table=TableResponse{SchemaVersion:"1.0",DocumentID:job.DocumentID,Page:page.Page}
            if err:=w.updatePageOutput(ctx,job,page.Page,"TABLES_SKIPPED","",""); err!=nil {
                return w.fail(ctx,job,err)
            }
        }

        extractions=append(extractions,pageExtraction{
            Page:page.Page,
            OCRBlocks:ocr.Blocks,
            TableHTML:table.PredHTML,
        })

        pageNumber:=page.Page
        progress:=pageProgress(50,25,index+1,native.PageCount)
        _=w.emitEvent(ctx,job,"page.table_completed","TABLE_PROCESSING",progress,&pageNumber,&native.PageCount,nil)
    }

    if err:=w.setStatus(ctx,job,"TABLES_COMPLETED",false,75); err!=nil { return w.fail(ctx,job,err) }
    if err:=w.setStatus(ctx,job,"MAPPING",false,80); err!=nil { return w.fail(ctx,job,err) }

    extractPayload:=map[string]any{
        "document_id":job.DocumentID,
        "pages":extractions,
    }
    if mappingConfig,ok:=job.Options["mapping_config"]; ok {
        extractPayload["mapping_config"]=mappingConfig
    }
    canonicalBody,err:=w.postJSON(ctx,"/v1/extract/invoice-document",extractPayload)
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("multi-page invoice extraction: %w",err)) }

    var canonical CanonicalResponse
    if err:=json.Unmarshal(canonicalBody,&canonical); err!=nil {
        return w.fail(ctx,job,fmt.Errorf("decode canonical result: %w",err))
    }
    if canonical.DocumentType=="" { canonical.DocumentType="invoice" }

    if err:=w.setStatus(ctx,job,"VALIDATING",false,90); err!=nil { return w.fail(ctx,job,err) }

    validationResult,err:=validation.ValidateInvoice(canonicalBody,"0.01")
    if err!=nil { return w.fail(ctx,job,fmt.Errorf("validate canonical invoice: %w",err)) }

    var agentTrace []byte
    if w.AgentFallbackEnabled && (canonical.RequiresReview || validationResult.RequiresReview) {
        resolved,trace,agentErr:=w.tryAgentFallback(ctx,job,canonicalBody,validationResult,extractions)
        if agentErr!=nil {
            w.Logger.Warn("agent_fallback_skipped","error",agentErr,"document_id",job.DocumentID)
        } else if len(resolved)>0 {
            candidateValidation,validateErr:=validation.ValidateInvoice(resolved,"0.01")
            if validateErr==nil {
                var traceEnvelope map[string]any
                _=json.Unmarshal(trace,&traceEnvelope)
                confidence,_:=traceEnvelope["confidence"].(float64)
                if confidence>=w.AgentMinConfidence && !candidateValidation.RequiresReview {
                    canonicalBody=resolved
                    validationResult=candidateValidation
                    canonical.RequiresReview=false
                    agentTrace=trace
                } else {
                    agentTrace=trace
                }
            }
        }
    }

    validationBody,err:=json.MarshalIndent(validationResult,"","  ")
    if err!=nil { return w.fail(ctx,job,err) }

    versionID,versionNumber,err:=w.createDocumentVersion(ctx,job,canonical.DocumentType)
    if err!=nil { return w.fail(ctx,job,err) }

    if err:=w.persistCanonicalRecords(ctx,job,versionID,canonicalBody,validationResult); err!=nil {
        return w.fail(ctx,job,fmt.Errorf("persist canonical records: %w",err))
    }

    finalDir:=filepath.Join(w.StorageRoot,"tenants",job.TenantID,"documents",job.DocumentID,versionID)
    canonicalPath,err:=writeArtifactFile(finalDir,"canonical.json",canonicalBody)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addVersionArtifact(ctx,job,versionID,"CANONICAL_JSON",canonicalPath,strPtr("application/json")); err!=nil {
        return w.fail(ctx,job,err)
    }

    if len(agentTrace)>0 {
        agentPath,agentErr:=writeArtifactFile(finalDir,"agent-trace.json",agentTrace)
        if agentErr!=nil { return w.fail(ctx,job,agentErr) }
        if err:=w.addVersionArtifact(ctx,job,versionID,"AGENT_TRACE_JSON",agentPath,strPtr("application/json")); err!=nil {
            return w.fail(ctx,job,err)
        }
        _=w.persistAgentRun(ctx,job,versionID,agentTrace)
    }

    validationPath,err:=writeArtifactFile(finalDir,"validation.json",validationBody)
    if err!=nil { return w.fail(ctx,job,err) }
    if err:=w.addVersionArtifact(ctx,job,versionID,"VALIDATION_JSON",validationPath,strPtr("application/json")); err!=nil {
        return w.fail(ctx,job,err)
    }

    requiresReview:=canonical.RequiresReview || validationResult.RequiresReview
    finalStatus:="READY_FOR_RECONCILIATION"
    eventType:="document.completed"
    if requiresReview {
        finalStatus="REVIEW_REQUIRED"
        eventType="review.required"
        _=w.createDocumentReviewTask(ctx,job,versionID,"extraction or validation requires review")
    }

    if err:=w.setStatus(ctx,job,finalStatus,true,100); err!=nil { return err }
    _=w.emitEvent(ctx,job,eventType,finalStatus,100,nil,&native.PageCount,map[string]any{
        "document_version_id":versionID,
        "validation_valid":validationResult.Valid,
        "requires_review":requiresReview,
    })

    if err:=w.completeStep(ctx,job); err!=nil { return err }

    w.Logger.Info("document_pipeline_completed",
        "tenant_id",job.TenantID,"document_id",job.DocumentID,"job_id",job.JobID,
        "document_version_id",versionID,"version_number",versionNumber,
        "page_count",native.PageCount,"status",finalStatus,"canonical_artifact",canonicalPath)
    return nil
}

func (w *Worker) beginStep(ctx context.Context,job JobMessage)(bool,error){
    step:=strings.TrimSpace(job.Step)
    if step=="" { step="PREPROCESS" }
    key:=job.JobID+":"+step

    tx,err:=w.DB.Begin(ctx)
    if err!=nil { return false,err }
    defer tx.Rollback(ctx)

    var status string
    var attempt int
    err=tx.QueryRow(ctx,`
      SELECT status,attempt_count
      FROM job_steps
      WHERE tenant_id=$1 AND job_id=$2 AND step_name=$3
      FOR UPDATE
    `,job.TenantID,job.JobID,step).Scan(&status,&attempt)

    if err==pgx.ErrNoRows {
        attempt=1
        if _,err=tx.Exec(ctx,`
          INSERT INTO job_steps
            (tenant_id,job_id,run_id,step_name,status,attempt_count,started_at,idempotency_key,updated_at)
          VALUES ($1,$2,$3,$4,'RUNNING',1,now(),$5,now())
        `,job.TenantID,job.JobID,job.RunID,step,key); err!=nil {
            return false,err
        }
    } else if err!=nil {
        return false,err
    } else if status=="COMPLETED" {
        if err=tx.Commit(ctx); err!=nil { return false,err }
        return true,nil
    } else {
        attempt++
        if _,err=tx.Exec(ctx,`
          UPDATE job_steps
          SET run_id=$1,status='RUNNING',attempt_count=$2,started_at=now(),
              completed_at=NULL,last_error=NULL,idempotency_key=$3,updated_at=now()
          WHERE tenant_id=$4 AND job_id=$5 AND step_name=$6
        `,job.RunID,attempt,key,job.TenantID,job.JobID,step); err!=nil {
            return false,err
        }
    }

    if _,err=tx.Exec(ctx,`
      INSERT INTO job_attempts
        (tenant_id,job_id,run_id,step_name,attempt_number,status,started_at)
      VALUES ($1,$2,$3,$4,$5,'RUNNING',now())
      ON CONFLICT (job_id,step_name,attempt_number) DO NOTHING
    `,job.TenantID,job.JobID,job.RunID,step,attempt); err!=nil {
        return false,err
    }

    if err=tx.Commit(ctx); err!=nil { return false,err }
    return false,nil
}

func (w *Worker) completeStep(ctx context.Context,job JobMessage) error {
    step:=strings.TrimSpace(job.Step)
    if step=="" { step="PREPROCESS" }

    tx,err:=w.DB.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    if _,err=tx.Exec(ctx,`
      UPDATE job_steps
      SET status='COMPLETED',completed_at=now(),last_error=NULL,updated_at=now()
      WHERE tenant_id=$1 AND job_id=$2 AND step_name=$3
    `,job.TenantID,job.JobID,step); err!=nil {
        return err
    }
    if _,err=tx.Exec(ctx,`
      UPDATE job_attempts
      SET status='COMPLETED',completed_at=now(),last_error=NULL
      WHERE tenant_id=$1 AND job_id=$2 AND step_name=$3
        AND attempt_number=(
          SELECT MAX(attempt_number) FROM job_attempts
          WHERE tenant_id=$1 AND job_id=$2 AND step_name=$3
        )
    `,job.TenantID,job.JobID,step); err!=nil {
        return err
    }
    return tx.Commit(ctx)
}

func (w *Worker) failStep(ctx context.Context,job JobMessage,cause error) error {
    step:=strings.TrimSpace(job.Step)
    if step=="" { step="PREPROCESS" }
    message:=cause.Error()

    tx,err:=w.DB.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    if _,err=tx.Exec(ctx,`
      UPDATE job_steps
      SET status='FAILED',completed_at=now(),last_error=$1,updated_at=now()
      WHERE tenant_id=$2 AND job_id=$3 AND step_name=$4
    `,message,job.TenantID,job.JobID,step); err!=nil {
        return err
    }
    if _,err=tx.Exec(ctx,`
      UPDATE job_attempts
      SET status='FAILED',completed_at=now(),last_error=$1
      WHERE tenant_id=$2 AND job_id=$3 AND step_name=$4
        AND attempt_number=(
          SELECT MAX(attempt_number) FROM job_attempts
          WHERE tenant_id=$2 AND job_id=$3 AND step_name=$4
        )
    `,message,job.TenantID,job.JobID,step); err!=nil {
        return err
    }
    return tx.Commit(ctx)
}

func pageProgress(base,span,index,total int) int {
    if total<=0 { return base }
    progress:=base+(span*index/total)
    if progress>100 { return 100 }
    return progress
}

func (w *Worker) postJSON(ctx context.Context,path string,payload any) ([]byte,error) {
    body,err:=json.Marshal(payload); if err!=nil { return nil,err }
    req,err:=http.NewRequestWithContext(ctx,http.MethodPost,w.AIEngineURL+path,bytes.NewReader(body))
    if err!=nil { return nil,err }
    req.Header.Set("Content-Type","application/json")
    resp,err:=w.HTTP.Do(req)
    if err!=nil { return nil,err }
    defer resp.Body.Close()

    responseBody,readErr:=io.ReadAll(io.LimitReader(resp.Body,64<<20))
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

func (w *Worker) persistCanonicalRecords(
    ctx context.Context,
    job JobMessage,
    versionID string,
    canonicalBody []byte,
    validationResult validation.Result,
) error {
    var invoice CanonicalInvoiceDocument
    if err:=json.Unmarshal(canonicalBody,&invoice); err!=nil { return err }

    tx,err:=w.DB.Begin(ctx)
    if err!=nil { return err }
    defer tx.Rollback(ctx)

    if _,err=tx.Exec(ctx,`
      DELETE FROM extracted_fields
      WHERE tenant_id=$1 AND document_version_id=$2
    `,job.TenantID,versionID); err!=nil {
        return err
    }
    if _,err=tx.Exec(ctx,`
      DELETE FROM document_line_items
      WHERE tenant_id=$1 AND document_version_id=$2
    `,job.TenantID,versionID); err!=nil {
        return err
    }
    if _,err=tx.Exec(ctx,`
      DELETE FROM validation_results
      WHERE tenant_id=$1 AND document_version_id=$2
    `,job.TenantID,versionID); err!=nil {
        return err
    }

    fields:=[]struct{
        Name string
        Type string
        Value *CanonicalField
    }{
        {"supplier_name","string",invoice.SupplierName},
        {"supplier_code","identifier",invoice.SupplierCode},
        {"invoice_number","identifier",invoice.InvoiceNumber},
        {"invoice_date","date",invoice.InvoiceDate},
        {"due_date","date",invoice.DueDate},
        {"po_number","identifier",invoice.PONumber},
        {"currency","currency",invoice.Currency},
        {"subtotal","decimal",invoice.Subtotal},
        {"tax","decimal",invoice.Tax},
        {"grand_total","decimal",invoice.GrandTotal},
    }

    for _,entry:=range fields {
        if entry.Value==nil || strings.TrimSpace(entry.Value.Value)=="" { continue }
        sourceMethod:=strings.TrimSpace(entry.Value.Evidence.Method)
        if sourceMethod=="" { sourceMethod="deterministic_extraction" }
        requiresReview:=entry.Value.Evidence.Confidence>0 && entry.Value.Evidence.Confidence<0.70

        var confidence any
        if entry.Value.Evidence.Confidence>0 { confidence=entry.Value.Evidence.Confidence }
        var page any
        if entry.Value.Evidence.Page>0 { page=entry.Value.Evidence.Page }

        evidenceJSON,_:=json.Marshal(map[string]any{
            "block_ids":entry.Value.Evidence.BlockIDs,
            "method":sourceMethod,
        })

        if _,err=tx.Exec(ctx,`
          INSERT INTO extracted_fields
            (tenant_id,document_version_id,field_name,raw_value,normalized_value,data_type,
             confidence,page_number,bbox_json,source_method,requires_review)
          VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9::jsonb,$10,$11)
        `,job.TenantID,versionID,entry.Name,entry.Value.Raw,entry.Value.Value,entry.Type,
          confidence,page,string(evidenceJSON),sourceMethod,requiresReview); err!=nil {
            return err
        }
    }

    for index,item:=range invoice.LineItems {
        encoded,marshalErr:=json.Marshal(item)
        if marshalErr!=nil { return marshalErr }
        if _,err=tx.Exec(ctx,`
          INSERT INTO document_line_items
            (tenant_id,document_version_id,row_number,data_json,requires_review)
          VALUES ($1,$2,$3,$4::jsonb,false)
        `,job.TenantID,versionID,index+1,string(encoded)); err!=nil {
            return err
        }
    }

    for _,rule:=range validationResult.Rules {
        evidence,_:=json.Marshal(rule.Evidence)
        if _,err=tx.Exec(ctx,`
          INSERT INTO validation_results
            (tenant_id,document_version_id,rule_code,status,severity,message,evidence_json)
          VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7::jsonb)
        `,job.TenantID,versionID,rule.Code,rule.Status,rule.Severity,rule.Message,string(evidence)); err!=nil {
            return err
        }
    }

    return tx.Commit(ctx)
}

func (w *Worker) createDocumentVersion(ctx context.Context,job JobMessage,documentType string)(string,int,error){
    var id string
    var version int
    var mappingTemplateID any
    var mappingTemplateVersion any
    if raw,ok:=job.Options["mapping_template_id"].(string); ok && raw!="" {
        mappingTemplateID=raw
    }
    switch value:=job.Options["mapping_template_version"].(type) {
    case float64:
        mappingTemplateVersion=int(value)
    case int:
        mappingTemplateVersion=value
    }

    tx,err:=w.DB.Begin(ctx)
    if err!=nil { return "",0,err }
    defer tx.Rollback(ctx)

    if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtext($1))`,job.DocumentID); err!=nil {
        return "",0,err
    }

    err=tx.QueryRow(ctx,`
      INSERT INTO document_versions
        (tenant_id,document_id,version_number,pipeline_version,mapping_template_id,mapping_template_version,source_run_id)
      SELECT $1,$2,COALESCE(MAX(version_number),0)+1,'mvp-0.5',$3,$4,$5
      FROM document_versions
      WHERE tenant_id=$1 AND document_id=$2
      ON CONFLICT (document_id,source_run_id) WHERE source_run_id IS NOT NULL
      DO UPDATE SET pipeline_version=EXCLUDED.pipeline_version
      RETURNING id::text,version_number
    `,job.TenantID,job.DocumentID,mappingTemplateID,mappingTemplateVersion,job.RunID).Scan(&id,&version)
    if err!=nil { return "",0,err }

    if _,err=tx.Exec(ctx,`
      UPDATE documents SET document_type=$1,updated_at=now()
      WHERE id=$2 AND tenant_id=$3
    `,documentType,job.DocumentID,job.TenantID); err!=nil {
        return "",0,err
    }
    if err=tx.Commit(ctx); err!=nil { return "",0,err }
    return id,version,nil
}

func (w *Worker) setStatus(ctx context.Context,job JobMessage,status string,completed bool,progress int) error {
    if completed {
        _,err:=w.DB.Exec(ctx,`
          UPDATE processing_jobs
          SET status=$1,started_at=COALESCE(started_at,now()),completed_at=now()
          WHERE id=$2 AND tenant_id=$3
        `,status,job.JobID,job.TenantID)
        if err!=nil { return err }
    } else {
        _,err:=w.DB.Exec(ctx,`
          UPDATE processing_jobs
          SET status=$1,started_at=COALESCE(started_at,now())
          WHERE id=$2 AND tenant_id=$3
        `,status,job.JobID,job.TenantID)
        if err!=nil { return err }
    }

    if _,err:=w.DB.Exec(ctx,`
      UPDATE documents SET status=$1,updated_at=now()
      WHERE id=$2 AND tenant_id=$3
    `,status,job.DocumentID,job.TenantID); err!=nil { return err }

    return w.emitEvent(ctx,job,"status.changed",status,progress,nil,nil,nil)
}

func (w *Worker) fail(ctx context.Context,job JobMessage,cause error) error {
    _=w.failStep(ctx,job,cause)
    _,_=w.DB.Exec(ctx,`
      UPDATE processing_jobs SET status='FAILED',failed_at=now()
      WHERE id=$1 AND tenant_id=$2
    `,job.JobID,job.TenantID)
    _,_=w.DB.Exec(ctx,`
      UPDATE documents SET status='FAILED',updated_at=now()
      WHERE id=$1 AND tenant_id=$2
    `,job.DocumentID,job.TenantID)
    _=w.emitEvent(ctx,job,"document.failed","FAILED",100,nil,nil,map[string]any{"error":cause.Error()})
    return cause
}

func (w *Worker) emitEvent(
    ctx context.Context,
    job JobMessage,
    eventType,status string,
    progress int,
    pageNumber,totalPages *int,
    data map[string]any,
) error {
    var dataJSON any
    if data!=nil {
        encoded,err:=json.Marshal(data)
        if err!=nil { return err }
        dataJSON=string(encoded)
    }

    _,err:=w.DB.Exec(ctx,`
      INSERT INTO job_events
        (tenant_id,job_id,document_id,event_type,status,progress,page_number,total_pages,data_json)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)
    `,job.TenantID,job.JobID,job.DocumentID,eventType,status,progress,pageNumber,totalPages,dataJSON)
    return err
}

func (w *Worker) upsertPage(
    ctx context.Context,
    job JobMessage,
    page NativePage,
    route,status,ocrArtifact,tableArtifact string,
) error {
    var textLayout any
    if page.TextLayoutArtifact!=nil { textLayout=*page.TextLayoutArtifact }

    _,err:=w.DB.Exec(ctx,`
      INSERT INTO processing_pages
        (tenant_id,job_id,document_id,run_id,page_number,route,searchable_text,status,
         image_artifact,text_layout_artifact,ocr_artifact,table_artifact)
      VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''))
      ON CONFLICT (job_id,page_number)
      DO UPDATE SET
        route=EXCLUDED.route,
        searchable_text=EXCLUDED.searchable_text,
        status=EXCLUDED.status,
        image_artifact=EXCLUDED.image_artifact,
        text_layout_artifact=EXCLUDED.text_layout_artifact,
        ocr_artifact=COALESCE(EXCLUDED.ocr_artifact,processing_pages.ocr_artifact),
        table_artifact=COALESCE(EXCLUDED.table_artifact,processing_pages.table_artifact),
        updated_at=now()
    `,job.TenantID,job.JobID,job.DocumentID,job.RunID,page.Page,route,page.SearchableText,status,
      page.ImageArtifact,textLayout,ocrArtifact,tableArtifact)
    return err
}

func (w *Worker) updatePageOutput(
    ctx context.Context,
    job JobMessage,
    page int,
    status,ocrArtifact,tableArtifact string,
) error {
    _,err:=w.DB.Exec(ctx,`
      UPDATE processing_pages
      SET status=$1,
          ocr_artifact=COALESCE(NULLIF($2,''),ocr_artifact),
          table_artifact=COALESCE(NULLIF($3,''),table_artifact),
          updated_at=now()
      WHERE tenant_id=$4 AND job_id=$5 AND page_number=$6
    `,status,ocrArtifact,tableArtifact,job.TenantID,job.JobID,page)
    return err
}

func (w *Worker) createDocumentReviewTask(ctx context.Context,job JobMessage,versionID,reason string) error {
    _,err:=w.DB.Exec(ctx,`
      INSERT INTO review_tasks
        (tenant_id,document_id,task_type,status,priority,reason)
      SELECT $1,$2,'DOCUMENT_VALIDATION','OPEN',50,$3
      WHERE NOT EXISTS (
        SELECT 1 FROM review_tasks
        WHERE tenant_id=$1 AND document_id=$2
          AND task_type='DOCUMENT_VALIDATION' AND status='OPEN'
      )
    `,job.TenantID,job.DocumentID,reason)
    return err
}

func (w *Worker) addArtifact(ctx context.Context,job JobMessage,kind,path string,contentType *string) error {
    info,err:=os.Stat(path); if err!=nil { return err }
    storageURI:=path
    if w.ObjectStore!=nil {
        content:="application/octet-stream"
        if contentType!=nil && *contentType!="" { content=*contentType }
        key:=filepath.ToSlash(filepath.Join("tenants",job.TenantID,"processing",job.DocumentID,job.RunID,strings.ToLower(kind),filepath.Base(path)))
        storageURI,err=w.ObjectStore.ArchiveFile(ctx,path,key,content)
        if err!=nil { return err }
    }
    _,err=w.DB.Exec(ctx,`
      INSERT INTO artifacts
        (tenant_id,document_id,run_id,artifact_type,storage_uri,content_type,size_bytes)
      SELECT $1,$2,$3,$4,$5,$6,$7
      WHERE NOT EXISTS (
        SELECT 1 FROM artifacts
        WHERE tenant_id=$1 AND run_id=$3 AND artifact_type=$4 AND storage_uri=$5
      )
    `,job.TenantID,job.DocumentID,job.RunID,kind,storageURI,contentType,info.Size())
    return err
}

func (w *Worker) addVersionArtifact(ctx context.Context,job JobMessage,versionID,kind,path string,contentType *string) error {
    info,err:=os.Stat(path); if err!=nil { return err }
    storageURI:=path
    if w.ObjectStore!=nil {
        content:="application/octet-stream"
        if contentType!=nil && *contentType!="" { content=*contentType }
        key:=filepath.ToSlash(filepath.Join("tenants",job.TenantID,"documents",job.DocumentID,versionID,filepath.Base(path)))
        storageURI,err=w.ObjectStore.ArchiveFile(ctx,path,key,content)
        if err!=nil { return err }
    }
    _,err=w.DB.Exec(ctx,`
      INSERT INTO artifacts
        (tenant_id,document_id,document_version_id,run_id,artifact_type,storage_uri,content_type,size_bytes)
      SELECT $1,$2,$3,$4,$5,$6,$7,$8
      WHERE NOT EXISTS (
        SELECT 1 FROM artifacts
        WHERE tenant_id=$1 AND document_version_id=$3 AND run_id=$4
          AND artifact_type=$5 AND storage_uri=$6
      )
    `,job.TenantID,job.DocumentID,versionID,job.RunID,kind,storageURI,contentType,info.Size())
    return err
}

func strPtr(v string)*string { return &v }


func (w *Worker) tryAgentFallback(
    ctx context.Context,job JobMessage,canonicalBody []byte,validationResult validation.Result,pages []pageExtraction,
)([]byte,[]byte,error){
    var canonical map[string]any
    if err:=json.Unmarshal(canonicalBody,&canonical); err!=nil { return nil,nil,err }
    validationJSON,err:=json.Marshal(validationResult)
    if err!=nil { return nil,nil,err }
    var validationMap map[string]any
    if err:=json.Unmarshal(validationJSON,&validationMap); err!=nil { return nil,nil,err }

    response,err:=w.postJSON(ctx,"/v1/agent/resolve-invoice",map[string]any{
        "document_id":job.DocumentID,
        "trigger_code":"DETERMINISTIC_REVIEW_REQUIRED",
        "canonical":canonical,
        "validation":validationMap,
        "pages":pages,
    })
    if err!=nil { return nil,nil,err }

    var envelope struct {
        Confidence float64 `json:"confidence"`
        ResolvedCanonical map[string]any `json:"resolved_canonical"`
    }
    if err:=json.Unmarshal(response,&envelope); err!=nil { return nil,nil,err }
    if envelope.Confidence<w.AgentMinConfidence || envelope.ResolvedCanonical==nil {
        return nil,response,nil
    }
    resolved,err:=json.MarshalIndent(envelope.ResolvedCanonical,"","  ")
    return resolved,response,err
}

func (w *Worker) persistAgentRun(ctx context.Context,job JobMessage,versionID string,trace []byte) error {
    var envelope struct {
        Provider string `json:"provider"`
        Model string `json:"model"`
        PromptVersion string `json:"prompt_version"`
        Confidence float64 `json:"confidence"`
    }
    if err:=json.Unmarshal(trace,&envelope); err!=nil { return err }
    _,err:=w.DB.Exec(ctx,`
      INSERT INTO agent_runs
        (tenant_id,document_id,document_version_id,run_id,trigger_code,provider,model,prompt_version,
         status,confidence,input_json,output_json,completed_at)
      VALUES ($1,$2,$3,$4,'DETERMINISTIC_REVIEW_REQUIRED',$5,$6,$7,'COMPLETED',$8,
              '{}'::jsonb,$9::jsonb,now())
    `,job.TenantID,job.DocumentID,versionID,job.RunID,envelope.Provider,envelope.Model,
      envelope.PromptVersion,envelope.Confidence,string(trace))
    return err
}
