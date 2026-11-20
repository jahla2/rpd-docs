package main

import (
    "context"
    "encoding/json"
    "log/slog"
    "os"
    "os/signal"
    "strconv"
    "syscall"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jahla2/rpd-docs/apps/orchestrator/internal/config"
    "github.com/jahla2/rpd-docs/apps/orchestrator/internal/objectstore"
    "github.com/jahla2/rpd-docs/apps/orchestrator/internal/pipeline"
    "github.com/nats-io/nats.go"
)

func main() {
    logger:=slog.New(slog.NewJSONHandler(os.Stdout,nil))
    cfg:=config.Load()

    db,err:=pgxpool.New(context.Background(),cfg.DatabaseURL)
    if err!=nil { logger.Error("postgres connect failed","error",err); os.Exit(1) }
    defer db.Close()
    if err:=db.Ping(context.Background()); err!=nil { logger.Error("postgres ping failed","error",err); os.Exit(1) }

    nc,err:=nats.Connect(cfg.NATSURL,nats.Name("rpd-orchestrator"))
    if err!=nil { logger.Error("nats connect failed","error",err); os.Exit(1) }
    defer nc.Close()
    js,err:=nc.JetStream()
    if err!=nil { logger.Error("jetstream failed","error",err); os.Exit(1) }
    if _,err:=js.StreamInfo("RPD_DOCUMENTS"); err!=nil {
        if _,addErr:=js.AddStream(&nats.StreamConfig{
            Name:"RPD_DOCUMENTS",Subjects:[]string{"document.*","reconciliation.*"},Storage:nats.FileStorage,
        }); addErr!=nil { logger.Error("ensure stream failed","error",addErr); os.Exit(1) }
    }

    obj,err:=objectstore.New(objectstore.Config{Backend:cfg.StorageBackend,Endpoint:cfg.S3Endpoint,Region:cfg.S3Region,Bucket:cfg.S3Bucket,AccessKey:cfg.S3AccessKey,SecretKey:cfg.S3SecretKey})
    if err!=nil { logger.Error("object storage startup failed","error",err); os.Exit(1) }
    minConfidence,_:=strconv.ParseFloat(cfg.AgentMinConfidence,64)
    worker:=pipeline.New(db,logger,cfg.StorageRoot,cfg.NativeEnginePath,cfg.AIEngineURL,cfg.AgentFallbackEnabled,minConfidence,obj)
    _,err=js.QueueSubscribe("document.preprocess","rpd-preprocess-workers",func(m *nats.Msg){
        var job pipeline.JobMessage
        if err:=json.Unmarshal(m.Data,&job); err!=nil {
            logger.Error("invalid job","error",err); _=m.Term(); return
        }
        ctx,cancel:=context.WithCancel(context.Background())
        defer cancel()

        heartbeatDone:=make(chan struct{})
        go func(){
            ticker:=time.NewTicker(15*time.Second)
            defer ticker.Stop()
            for {
                select {
                case <-heartbeatDone:
                    return
                case <-ticker.C:
                    _=m.InProgress()
                }
            }
        }()
        defer close(heartbeatDone)

        if err:=worker.HandlePreprocess(ctx,job); err!=nil {
            deliveries:=uint64(1)
            if metadata,metadataErr:=m.Metadata(); metadataErr==nil {
                deliveries=metadata.NumDelivered
            }
            logger.Error("preprocess pipeline failed","error",err,"job_id",job.JobID,"document_id",job.DocumentID,"delivery",deliveries)
            if deliveries<3 {
                _=worker.MarkRetrying(context.Background(),job,err)
                _=m.Nak()
            } else {
                _=m.Term()
            }
            return
        }
        _=m.Ack()
    },nats.Durable("rpd-preprocess"),nats.ManualAck(),nats.AckExplicit(),nats.AckWait(60*time.Second),nats.MaxDeliver(3))
    if err!=nil { logger.Error("subscribe failed","error",err); os.Exit(1) }

    logger.Info("orchestrator_started","native_engine",cfg.NativeEnginePath,"ai_engine",cfg.AIEngineURL)
    stop:=make(chan os.Signal,1); signal.Notify(stop,syscall.SIGINT,syscall.SIGTERM); <-stop
}
