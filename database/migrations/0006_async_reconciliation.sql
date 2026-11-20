BEGIN;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE reconciliation_runs
    ADD COLUMN IF NOT EXISTS progress integer NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    ADD COLUMN IF NOT EXISTS cancel_requested boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS document_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS processed_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS matched_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS suggested_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS exception_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS failed_at timestamptz NULL,
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE IF NOT EXISTS reconciliation_run_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'QUEUED',
    attempt_count integer NOT NULL DEFAULT 0,
    last_error text NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(reconciliation_run_id, document_id)
);

CREATE INDEX IF NOT EXISTS ix_reconciliation_run_items_pending
    ON reconciliation_run_items(tenant_id, reconciliation_run_id, status, created_at);

CREATE TABLE IF NOT EXISTS reconciliation_events (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    status text NOT NULL,
    progress integer NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    document_id uuid NULL REFERENCES documents(id) ON DELETE SET NULL,
    data_json jsonb NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_reconciliation_events_stream
    ON reconciliation_events(tenant_id, reconciliation_run_id, id);

ALTER TABLE suppliers
    ADD COLUMN IF NOT EXISTS normalized_name text NULL;

UPDATE suppliers
SET normalized_name = upper(regexp_replace(supplier_name, '[^[:alnum:]]+', '', 'g'))
WHERE normalized_name IS NULL;

CREATE INDEX IF NOT EXISTS ix_suppliers_normalized_name
    ON suppliers(tenant_id, normalized_name);

CREATE INDEX IF NOT EXISTS ix_suppliers_name_trgm
    ON suppliers USING gin (supplier_name gin_trgm_ops);

CREATE UNIQUE INDEX IF NOT EXISTS ux_supplier_alias_normalized
    ON supplier_aliases(tenant_id, normalized_alias);

CREATE INDEX IF NOT EXISTS ix_supplier_alias_trgm
    ON supplier_aliases USING gin (alias gin_trgm_ops);

CREATE TABLE IF NOT EXISTS reconciliation_match_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    group_type text NOT NULL,
    status text NOT NULL,
    score numeric(6,2) NOT NULL,
    source_total numeric(20,4) NOT NULL,
    target_total numeric(20,4) NOT NULL,
    difference_amount numeric(20,4) NOT NULL,
    evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reconciliation_match_group_members (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    group_id uuid NOT NULL REFERENCES reconciliation_match_groups(id) ON DELETE CASCADE,
    side text NOT NULL CHECK (side IN ('SOURCE','TARGET')),
    source_document_id uuid NULL REFERENCES documents(id) ON DELETE CASCADE,
    target_record_id uuid NULL REFERENCES erp_records(id) ON DELETE CASCADE,
    amount numeric(20,4) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (side='SOURCE' AND source_document_id IS NOT NULL AND target_record_id IS NULL)
        OR
        (side='TARGET' AND target_record_id IS NOT NULL AND source_document_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS ix_reconciliation_group_members
    ON reconciliation_match_group_members(tenant_id, group_id, side);

COMMIT;
