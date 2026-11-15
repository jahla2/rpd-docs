package main

import (
    "context"
    "encoding/json"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/config"
    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/reconservice"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
    "github.com/nats-io/nats.go"
)

func main(){
    logger:=slog.New(slog.NewJSONHandler(os.Stdout,nil))
    cfg:=config.Load()

    st,err:=store.New(context.Background(),cfg.DatabaseURL)
    if err!=nil { logger.Error("postgres startup failed","error",err); os.Exit(1) }
    defer st.Close()

    nc,err:=nats.Connect(cfg.NATSURL,nats.Name("rpd-reconciliation-worker"))
    if err!=nil { logger.Error("nats startup failed","error",err); os.Exit(1) }
    defer nc.Close()
    js,err:=nc.JetStream()
    if err!=nil { logger.Error("jetstream startup failed","error",err); os.Exit(1) }

    // API startup normally creates/updates the stream. The worker retries the
    // subscription briefly so deploy order does not create a race.
    var sub *nats.Subscription
    for attempt:=0; attempt<20; attempt++ {
        sub,err=js.QueueSubscribe("reconciliation.run","rpd-reconciliation-workers",func(m *nats.Msg){
            var message queue.ReconciliationJobMessage
            if decodeErr:=json.Unmarshal(m.Data,&message); decodeErr!=nil {
                logger.Error("invalid reconciliation message","error",decodeErr)
                _=m.Term()
                return
            }
            if message.SchemaVersion!="1.0" || message.TenantID=="" || message.RunID=="" {
                logger.Error("invalid reconciliation contract","run_id",message.RunID)
                _=m.Term()
                return
            }

            service:=reconservice.New(st)
            ctx,cancel:=context.WithCancel(context.Background())
            defer cancel()

            if executeErr:=service.ExecuteQueuedRun(ctx,message.TenantID,message.RunID); executeErr!=nil {
                logger.Error("reconciliation run failed","error",executeErr,"run_id",message.RunID)
                _=st.FailReconciliationRun(context.Background(),message.TenantID,message.RunID,executeErr)
                _=m.Nak()
                return
            }
            _=m.Ack()
        },nats.Durable("rpd-reconciliation-run"),nats.ManualAck(),nats.AckExplicit(),nats.MaxDeliver(3))
        if err==nil { break }
        time.Sleep(500*time.Millisecond)
    }
    if err!=nil { logger.Error("subscribe failed","error",err); os.Exit(1) }
    defer sub.Unsubscribe()

    logger.Info("reconciliation_worker_started")
    stop:=make(chan os.Signal,1)
    signal.Notify(stop,syscall.SIGINT,syscall.SIGTERM)
    <-stop
}
