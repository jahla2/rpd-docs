CREATE TABLE IF NOT EXISTS layout_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  document_type text NOT NULL,
  supplier_key text NOT NULL DEFAULT '',
  name text NOT NULL,
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  status text NOT NULL DEFAULT 'CANDIDATE'
    CHECK (status IN ('CANDIDATE','ACTIVE','ARCHIVED')),
  fingerprint_hash text NOT NULL,
  fingerprint_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  observation_count integer NOT NULL DEFAULT 1 CHECK (observation_count > 0),
  source text NOT NULL DEFAULT 'AUTO_VALIDATED',
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, document_type, fingerprint_hash)
);

CREATE INDEX IF NOT EXISTS idx_layout_profiles_active_lookup
  ON layout_profiles (tenant_id, document_type, status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS idx_layout_profiles_supplier_lookup
  ON layout_profiles (tenant_id, supplier_key, document_type, status)
  WHERE supplier_key <> '';
