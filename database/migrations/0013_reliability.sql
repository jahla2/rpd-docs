BEGIN;

ALTER TABLE job_steps
    ADD COLUMN IF NOT EXISTS run_id uuid NULL,
    ADD COLUMN IF NOT EXISTS idempotency_key text NULL,
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

UPDATE job_steps js
SET run_id = pj.run_id
FROM processing_jobs pj
WHERE js.job_id = pj.id
  AND js.run_id IS NULL;

UPDATE job_steps
SET idempotency_key = job_id::text || ':' || step_name
WHERE idempotency_key IS NULL;

ALTER TABLE job_steps
    ALTER COLUMN run_id SET NOT NULL,
    ALTER COLUMN idempotency_key SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_job_steps_idempotency
    ON job_steps(tenant_id,idempotency_key);

CREATE TABLE IF NOT EXISTS job_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    step_name text NOT NULL,
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    status text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    last_error text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(job_id,step_name,attempt_number)
);

CREATE INDEX IF NOT EXISTS ix_job_attempts_lookup
    ON job_attempts(tenant_id,job_id,step_name,attempt_number DESC);

ALTER TABLE document_versions
    ADD COLUMN IF NOT EXISTS source_run_id uuid NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_document_versions_source_run
    ON document_versions(document_id,source_run_id)
    WHERE source_run_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_processing_jobs_tenant_run
    ON processing_jobs(tenant_id,run_id);

COMMIT;
