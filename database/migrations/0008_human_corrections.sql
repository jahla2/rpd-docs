BEGIN;

ALTER TABLE document_versions
    ADD COLUMN IF NOT EXISTS source_version_id uuid NULL REFERENCES document_versions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS correction_reason text NULL,
    ADD COLUMN IF NOT EXISTS corrected_by text NULL;

ALTER TABLE review_tasks
    ADD COLUMN IF NOT EXISTS document_version_id uuid NULL REFERENCES document_versions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS ix_document_versions_source
    ON document_versions(tenant_id, document_id, source_version_id);

COMMIT;
