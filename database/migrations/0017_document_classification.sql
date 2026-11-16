BEGIN;

CREATE TABLE IF NOT EXISTS document_classifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    document_type text NOT NULL,
    confidence numeric(6,5) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    method text NOT NULL,
    model text NULL,
    model_version text NULL,
    alternatives_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    latency_ms bigint NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    requires_review boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(job_id)
);

CREATE INDEX IF NOT EXISTS ix_document_classifications_document
    ON document_classifications(tenant_id,document_id,created_at DESC);

COMMIT;
