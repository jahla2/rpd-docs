package main

import (
    "encoding/json"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "github.com/nats-io/nats.go"
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

func main() {
    logger:=slog.New(slog.NewJSONHandler(os.Stdout,nil))
    nc,err:=nats.Connect(getenv("NATS_URL","nats://localhost:4222"),nats.Name("rpd-orchestrator"))
    if err != nil { logger.Error("nats connect failed","error",err); os.Exit(1) }
    defer nc.Close()
    js,err:=nc.JetStream()
    if err != nil { logger.Error("jetstream failed","error",err); os.Exit(1) }

    _,err=js.QueueSubscribe("document.preprocess","rpd-preprocess-workers",func(m *nats.Msg){
        var job JobMessage
        if err:=json.Unmarshal(m.Data,&job); err != nil {
            logger.Error("invalid job","error",err); _=m.Term(); return
        }
        logger.Info("preprocess_job_received",
            "tenant_id",job.TenantID,"document_id",job.DocumentID,
            "job_id",job.JobID,"run_id",job.RunID,"input_artifact",job.InputArtifact)
        // Phase 1: contract validation + durable queue consumption.
        // Phase 2 invokes the C++ engine and dispatches document.ocr.
        _=m.Ack()
    },nats.Durable("rpd-preprocess"),nats.ManualAck(),nats.AckExplicit())
    if err != nil { logger.Error("subscribe failed","error",err); os.Exit(1) }

    logger.Info("orchestrator_started")
    stop:=make(chan os.Signal,1); signal.Notify(stop,syscall.SIGINT,syscall.SIGTERM); <-stop
}
func getenv(key,fallback string) string { if v:=os.Getenv(key); v!="" { return v }; return fallback }
