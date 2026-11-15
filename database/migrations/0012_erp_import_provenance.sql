BEGIN;

CREATE TABLE IF NOT EXISTS erp_imports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    dataset_id uuid NOT NULL REFERENCES erp_datasets(id) ON DELETE CASCADE,
    original_filename text NOT NULL,
    storage_uri text NOT NULL,
    sha256 char(64) NOT NULL,
    size_bytes bigint NOT NULL,
    status text NOT NULL,
    mapping_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    valid_rows integer NOT NULL DEFAULT 0,
    inserted_rows integer NOT NULL DEFAULT 0,
    duplicate_rows integer NOT NULL DEFAULT 0,
    error_count integer NOT NULL DEFAULT 0,
    errors_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    error_message text NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ix_erp_imports_dataset
    ON erp_imports(tenant_id,dataset_id,created_at DESC);

COMMIT;
