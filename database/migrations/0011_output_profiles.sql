BEGIN;

CREATE TABLE IF NOT EXISTS output_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    format text NOT NULL CHECK (format IN ('json','csv','xlsx')),
    columns_json jsonb NOT NULL,
    header_aliases_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    active boolean NOT NULL DEFAULT true,
    created_by text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,name)
);

CREATE INDEX IF NOT EXISTS ix_output_profiles_active
    ON output_profiles(tenant_id,active,name);

COMMIT;
