BEGIN;

ALTER TABLE mapping_templates
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS activated_at timestamptz NULL;

CREATE UNIQUE INDEX IF NOT EXISTS ux_mapping_templates_active
    ON mapping_templates(tenant_id, name, document_type_id)
    WHERE status='ACTIVE';

CREATE INDEX IF NOT EXISTS ix_mapping_templates_lookup
    ON mapping_templates(tenant_id, document_type_id, status, name, version DESC);

COMMIT;
