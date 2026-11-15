BEGIN;

ALTER TABLE erp_records
    ADD COLUMN IF NOT EXISTS supplier_name text NULL,
    ADD COLUMN IF NOT EXISTS normalized_supplier text NULL,
    ADD COLUMN IF NOT EXISTS source_row integer NULL,
    ADD COLUMN IF NOT EXISTS row_hash char(64) NULL;

CREATE INDEX IF NOT EXISTS ix_erp_supplier_candidate
    ON erp_records(tenant_id, dataset_id, normalized_supplier, currency, record_date, amount);

CREATE UNIQUE INDEX IF NOT EXISTS ux_erp_dataset_row_hash
    ON erp_records(tenant_id, dataset_id, row_hash)
    WHERE row_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS exports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    reconciliation_run_id uuid NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    format text NOT NULL,
    status text NOT NULL DEFAULT 'COMPLETED',
    storage_uri text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_exports_run
    ON exports(tenant_id, reconciliation_run_id, created_at DESC);

COMMIT;
