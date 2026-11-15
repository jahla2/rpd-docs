BEGIN;

CREATE TABLE IF NOT EXISTS processing_pages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    page_number integer NOT NULL CHECK (page_number > 0),
    route text NOT NULL,
    searchable_text boolean NOT NULL DEFAULT false,
    status text NOT NULL,
    image_artifact text NULL,
    text_layout_artifact text NULL,
    ocr_artifact text NULL,
    table_artifact text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(job_id, page_number)
);

CREATE INDEX IF NOT EXISTS ix_processing_pages_job
    ON processing_pages(tenant_id, job_id, page_number);

CREATE TABLE IF NOT EXISTS job_events (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    status text NOT NULL,
    progress integer NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    page_number integer NULL,
    total_pages integer NULL,
    data_json jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_job_events_stream
    ON job_events(tenant_id, job_id, id);

COMMIT;
