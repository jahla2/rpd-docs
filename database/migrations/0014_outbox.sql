BEGIN;

CREATE TABLE IF NOT EXISTS outbox_messages (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    subject text NOT NULL,
    message_key text NOT NULL UNIQUE,
    payload_json jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING',
    attempt_count integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz NULL,
    last_error text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('PENDING','PUBLISHING','PUBLISHED'))
);

CREATE INDEX IF NOT EXISTS ix_outbox_pending
    ON outbox_messages(status,available_at,id)
    WHERE status <> 'PUBLISHED';

COMMIT;
