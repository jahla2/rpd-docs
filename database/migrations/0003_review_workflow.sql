BEGIN;

ALTER TABLE review_tasks
    ADD COLUMN IF NOT EXISTS source_record_id text NULL,
    ADD COLUMN IF NOT EXISTS resolution_json jsonb NULL,
    ADD COLUMN IF NOT EXISTS resolved_by text NULL;

CREATE INDEX IF NOT EXISTS ix_review_tasks_open
    ON review_tasks(tenant_id, status, created_at)
    WHERE status='OPEN';

COMMIT;
