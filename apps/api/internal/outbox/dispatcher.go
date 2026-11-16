package outbox

import (
    "context"
    "log/slog"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/queue"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

type Dispatcher struct {
    Store *store.Store
    Queue *queue.Publisher
    Logger *slog.Logger
    Interval time.Duration
    BatchSize int
}

func (d *Dispatcher) Run(ctx context.Context) {
    interval:=d.Interval
    if interval<=0 { interval=500*time.Millisecond }
    batch:=d.BatchSize
    if batch<=0 { batch=25 }

    ticker:=time.NewTicker(interval)
    defer ticker.Stop()

    d.dispatch(ctx,batch)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            d.dispatch(ctx,batch)
        }
    }
}

func (d *Dispatcher) dispatch(ctx context.Context,batch int) {
    items,err:=d.Store.ClaimOutboxMessages(ctx,batch)
    if err!=nil {
        if ctx.Err()==nil && d.Logger!=nil {
            d.Logger.Error("outbox_claim_failed","error",err)
        }
        return
    }

    for _,item:=range items {
        if err:=d.Queue.PublishRaw(item.Subject,item.Payload,item.MessageKey); err!=nil {
            if d.Logger!=nil {
                d.Logger.Warn("outbox_publish_failed",
                    "error",err,"outbox_id",item.ID,"subject",item.Subject,
                    "message_key",item.MessageKey,"attempt",item.AttemptCount)
            }
            _=d.Store.MarkOutboxFailed(ctx,item.ID,err)
            continue
        }
        if err:=d.Store.MarkOutboxPublished(ctx,item.ID); err!=nil && d.Logger!=nil {
            d.Logger.Error("outbox_mark_published_failed",
                "error",err,"outbox_id",item.ID,"message_key",item.MessageKey)
        }
    }
}
