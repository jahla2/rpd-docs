BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO tenants (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'Default')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    original_filename text NOT NULL,
    mime_type text NOT NULL,
    sha256 char(64) NOT NULL,
    document_type text NOT NULL DEFAULT 'unknown',
    status text NOT NULL DEFAULT 'RECEIVED',
    storage_uri text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_documents_tenant_sha ON documents(tenant_id, sha256);
CREATE INDEX IF NOT EXISTS ix_documents_tenant_status ON documents(tenant_id, status);

CREATE TABLE IF NOT EXISTS document_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version_number integer NOT NULL,
    pipeline_version text NOT NULL,
    mapping_template_id uuid NULL,
    mapping_template_version integer NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(document_id, version_number)
);

CREATE TABLE IF NOT EXISTS processing_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'QUEUED',
    priority integer NOT NULL DEFAULT 100,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    failed_at timestamptz NULL
);
CREATE INDEX IF NOT EXISTS ix_jobs_tenant_status ON processing_jobs(tenant_id, status);

CREATE TABLE IF NOT EXISTS job_steps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    job_id uuid NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    step_name text NOT NULL,
    status text NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    last_error text NULL,
    UNIQUE(job_id, step_name)
);

CREATE TABLE IF NOT EXISTS artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    document_version_id uuid NULL REFERENCES document_versions(id) ON DELETE SET NULL,
    run_id uuid NULL,
    artifact_type text NOT NULL,
    storage_uri text NOT NULL,
    content_type text NULL,
    size_bytes bigint NULL,
    sha256 char(64) NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS document_types (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    code text NOT NULL,
    name text NOT NULL,
    schema_version integer NOT NULL DEFAULT 1,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, code, schema_version)
);

CREATE TABLE IF NOT EXISTS field_definitions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_type_id uuid NOT NULL REFERENCES document_types(id) ON DELETE CASCADE,
    field_name text NOT NULL,
    display_name text NOT NULL,
    data_type text NOT NULL,
    required boolean NOT NULL DEFAULT false,
    repeating boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(document_type_id, field_name)
);

CREATE TABLE IF NOT EXISTS mapping_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    document_type_id uuid NOT NULL REFERENCES document_types(id),
    version integer NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT',
    config_json jsonb NOT NULL,
    created_by text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name, version)
);

CREATE TABLE IF NOT EXISTS extracted_fields (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_version_id uuid NOT NULL REFERENCES document_versions(id) ON DELETE CASCADE,
    field_name text NOT NULL,
    raw_value text NULL,
    normalized_value text NULL,
    data_type text NOT NULL,
    confidence numeric(6,5) NULL,
    page_number integer NULL,
    bbox_json jsonb NULL,
    source_method text NOT NULL,
    requires_review boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_extracted_fields_lookup
    ON extracted_fields(tenant_id, document_version_id, field_name);

CREATE TABLE IF NOT EXISTS document_line_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_version_id uuid NOT NULL REFERENCES document_versions(id) ON DELETE CASCADE,
    row_number integer NOT NULL,
    data_json jsonb NOT NULL,
    confidence numeric(6,5) NULL,
    requires_review boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS validation_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_version_id uuid NOT NULL REFERENCES document_versions(id) ON DELETE CASCADE,
    rule_code text NOT NULL,
    status text NOT NULL,
    severity text NOT NULL,
    message text NULL,
    evidence_json jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS suppliers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    supplier_code text NULL,
    supplier_name text NOT NULL,
    tax_id text NULL,
    status text NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_suppliers_name ON suppliers(tenant_id, supplier_name);

CREATE TABLE IF NOT EXISTS supplier_aliases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    supplier_id uuid NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    alias text NOT NULL,
    normalized_alias text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS erp_datasets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    source_type text NOT NULL,
    status text NOT NULL DEFAULT 'READY',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS erp_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    dataset_id uuid NOT NULL REFERENCES erp_datasets(id) ON DELETE CASCADE,
    external_record_id text NULL,
    record_date date NULL,
    supplier_id uuid NULL REFERENCES suppliers(id),
    reference text NULL,
    normalized_reference text NULL,
    currency char(3) NULL,
    amount numeric(20,4) NULL,
    data_json jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_erp_candidate
    ON erp_records(tenant_id, dataset_id, supplier_id, record_date, amount);
CREATE INDEX IF NOT EXISTS ix_erp_reference
    ON erp_records(tenant_id, dataset_id, normalized_reference);

CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    source_dataset text NOT NULL,
    target_dataset_id uuid NOT NULL REFERENCES erp_datasets(id),
    status text NOT NULL DEFAULT 'QUEUED',
    rule_set_version text NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS matches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    source_record_id text NOT NULL,
    target_record_id uuid NOT NULL REFERENCES erp_records(id),
    status text NOT NULL,
    score numeric(6,2) NOT NULL,
    matching_rule text NOT NULL,
    difference_amount numeric(20,4) NULL,
    evidence_json jsonb NOT NULL,
    approved_by text NULL,
    approved_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reconciliation_exceptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    source_record_id text NOT NULL,
    exception_type text NOT NULL,
    severity text NOT NULL,
    message text NULL,
    evidence_json jsonb NULL,
    status text NOT NULL DEFAULT 'OPEN',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS review_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    document_id uuid NULL REFERENCES documents(id),
    reconciliation_run_id uuid NULL REFERENCES reconciliation_runs(id),
    task_type text NOT NULL,
    status text NOT NULL DEFAULT 'OPEN',
    assigned_to text NULL,
    priority integer NOT NULL DEFAULT 100,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    actor_type text NOT NULL,
    actor_id text NULL,
    event_type text NOT NULL,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    before_json jsonb NULL,
    after_json jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_audit_entity
    ON audit_events(tenant_id, entity_type, entity_id, created_at DESC);

CREATE TABLE IF NOT EXISTS model_registry (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    model_type text NOT NULL,
    version text NOT NULL,
    provider text NOT NULL,
    checksum text NULL,
    config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(name, version)
);

COMMIT;
