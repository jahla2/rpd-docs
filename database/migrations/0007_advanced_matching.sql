BEGIN;

CREATE INDEX IF NOT EXISTS ix_erp_supplier_name_trgm
    ON erp_records USING gin (supplier_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS ix_erp_normalized_supplier
    ON erp_records(tenant_id, dataset_id, normalized_supplier);

CREATE INDEX IF NOT EXISTS ix_erp_group_candidate
    ON erp_records(tenant_id, dataset_id, currency, record_date, amount)
    WHERE amount IS NOT NULL;

COMMIT;
