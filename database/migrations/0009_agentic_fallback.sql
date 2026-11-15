BEGIN;

CREATE TABLE IF NOT EXISTS agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    document_version_id uuid NULL REFERENCES document_versions(id) ON DELETE SET NULL,
    run_id uuid NULL,
    trigger_code text NOT NULL,
    provider text NOT NULL,
    model text NOT NULL,
    prompt_version text NOT NULL,
    status text NOT NULL,
    confidence numeric(6,5) NULL,
    input_json jsonb NOT NULL,
    output_json jsonb NULL,
    error_message text NULL,
    latency_ms integer NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS ix_agent_runs_document
    ON agent_runs(tenant_id, document_id, created_at DESC);

COMMIT;
