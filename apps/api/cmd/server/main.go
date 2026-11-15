package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/auth"
    "github.com/jahla2/rpd-docs/apps/api/internal/config"
    "github.com/jahla2/rpd-docs/apps/api/internal/httpapi"
    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/storage"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

func main() {
    logger:=slog.New(slog.NewJSONHandler(os.Stdout,nil))
    cfg:=config.Load()
    st,err:=store.New(context.Background(),cfg.DatabaseURL)
    if err != nil { logger.Error("postgres startup failed","error",err); os.Exit(1) }
    defer st.Close()
    q,err:=queue.NewPublisher(cfg.NATSURL)
    if err != nil { logger.Error("nats startup failed","error",err); os.Exit(1) }
    defer q.Close()

    storageManager,err:=storage.NewManager(storage.Config{
        Backend:cfg.StorageBackend,Root:cfg.StorageRoot,Endpoint:cfg.S3Endpoint,Region:cfg.S3Region,
        Bucket:cfg.S3Bucket,AccessKey:cfg.S3AccessKey,SecretKey:cfg.S3SecretKey,CreateBucket:cfg.S3CreateBucket,
    })
    if err!=nil { logger.Error("storage startup failed","error",err); os.Exit(1) }
    if err:=storageManager.Ready(); err!=nil { logger.Error("storage readiness failed","error",err); os.Exit(1) }
    handler:=httpapi.New(st,storageManager,q,cfg.DefaultTenantID,cfg.APIKeyPepper,logger)
    handler=auth.Middleware{Store:st,Mode:cfg.AuthMode,Pepper:cfg.APIKeyPepper,Logger:logger}.Wrap(handler)
    srv:=&http.Server{
        Addr:cfg.HTTPAddr,
        Handler:handler,
        ReadHeaderTimeout:10*time.Second,
    }
    go func(){
        logger.Info("api_started","addr",cfg.HTTPAddr)
        if err:=srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("api_failed","error",err); os.Exit(1)
        }
    }()
    stop:=make(chan os.Signal,1); signal.Notify(stop,syscall.SIGINT,syscall.SIGTERM); <-stop
    ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second); defer cancel()
    _=srv.Shutdown(ctx)
}
