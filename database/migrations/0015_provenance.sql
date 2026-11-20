BEGIN;

ALTER TABLE extracted_fields
    ADD COLUMN IF NOT EXISTS evidence_json jsonb NULL;

CREATE TABLE IF NOT EXISTS ocr_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    page_number integer NOT NULL CHECK (page_number > 0),
    engine text NOT NULL,
    model text NULL,
    model_version text NULL,
    device text NULL,
    block_count integer NOT NULL DEFAULT 0,
    latency_ms integer NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    UNIQUE(job_id,page_number)
);

CREATE INDEX IF NOT EXISTS ix_ocr_runs_document
    ON ocr_runs(tenant_id,document_id,created_at DESC);

COMMIT;
