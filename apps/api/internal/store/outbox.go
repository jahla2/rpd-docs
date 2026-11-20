package store

import (
    "context"
    "encoding/json"
    "time"
)

type OutboxMessage struct {
    ID int64
    TenantID string
    Subject string
    MessageKey string
    Payload json.RawMessage
    AttemptCount int
    AvailableAt time.Time
}

func (s *Store) ClaimOutboxMessages(ctx context.Context,limit int)([]OutboxMessage,error){
    if limit<=0 || limit>100 { limit=25 }

    rows,err:=s.pool.Query(ctx,`
      WITH candidates AS (
        SELECT id
        FROM outbox_messages
        WHERE (
          status='PENDING'
          OR (status='PUBLISHING' AND available_at<=now())
        )
          AND available_at<=now()
        ORDER BY id
        FOR UPDATE SKIP LOCKED
        LIMIT $1
      )
      UPDATE outbox_messages o
      SET status='PUBLISHING',
          attempt_count=o.attempt_count+1,
          available_at=now()+interval '30 seconds',
          updated_at=now()
      FROM candidates c
      WHERE o.id=c.id
      RETURNING o.id,o.tenant_id::text,o.subject,o.message_key,o.payload_json,
                o.attempt_count,o.available_at
    `,limit)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]OutboxMessage,0)
    for rows.Next(){
        var item OutboxMessage
        if err:=rows.Scan(
            &item.ID,&item.TenantID,&item.Subject,&item.MessageKey,&item.Payload,
            &item.AttemptCount,&item.AvailableAt,
        ); err!=nil {
            return nil,err
        }
        out=append(out,item)
    }
    return out,rows.Err()
}

func (s *Store) MarkOutboxPublished(ctx context.Context,id int64) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE outbox_messages
      SET status='PUBLISHED',published_at=now(),last_error=NULL,updated_at=now()
      WHERE id=$1
    `,id)
    return err
}

func (s *Store) MarkOutboxPublishedByKey(ctx context.Context,messageKey string) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE outbox_messages
      SET status='PUBLISHED',published_at=COALESCE(published_at,now()),
          last_error=NULL,updated_at=now()
      WHERE message_key=$1
    `,messageKey)
    return err
}

func (s *Store) MarkOutboxFailed(ctx context.Context,id int64,cause error) error {
    message:=""
    if cause!=nil { message=cause.Error() }
    _,err:=s.pool.Exec(ctx,`
      UPDATE outbox_messages
      SET status='PENDING',
          available_at=now()+interval '5 seconds',
          last_error=NULLIF($2,''),
          updated_at=now()
      WHERE id=$1
    `,id,message)
    return err
}
