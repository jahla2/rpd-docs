BEGIN;

CREATE TABLE IF NOT EXISTS permissions (
    code text PRIMARY KEY,
    description text NOT NULL
);

INSERT INTO permissions(code,description) VALUES
    ('document:read','Read documents, jobs and artifacts'),
    ('document:write','Upload and correct documents'),
    ('config:read','Read mappings, suppliers and datasets'),
    ('config:write','Manage mappings, suppliers and datasets'),
    ('reconciliation:read','Read reconciliation runs and exports'),
    ('reconciliation:write','Create, cancel and export reconciliation runs'),
    ('review:write','Approve, reject and resolve review work'),
    ('admin:keys','Manage API keys')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS roles (
    code text PRIMARY KEY,
    name text NOT NULL
);

INSERT INTO roles(code,name) VALUES
    ('ADMIN','Administrator'),
    ('OPERATOR','Operator'),
    ('REVIEWER','Reviewer'),
    ('READER','Read only')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS role_permissions (
    role_code text NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    permission_code text NOT NULL REFERENCES permissions(code) ON DELETE CASCADE,
    PRIMARY KEY(role_code,permission_code)
);

INSERT INTO role_permissions(role_code,permission_code)
SELECT 'ADMIN',code FROM permissions
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OPERATOR','document:read'),
    ('OPERATOR','document:write'),
    ('OPERATOR','config:read'),
    ('OPERATOR','config:write'),
    ('OPERATOR','reconciliation:read'),
    ('OPERATOR','reconciliation:write'),
    ('REVIEWER','document:read'),
    ('REVIEWER','config:read'),
    ('REVIEWER','reconciliation:read'),
    ('REVIEWER','review:write'),
    ('READER','document:read'),
    ('READER','config:read'),
    ('READER','reconciliation:read')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    key_prefix text NOT NULL UNIQUE,
    secret_hash char(64) NOT NULL,
    role_code text NOT NULL REFERENCES roles(code),
    status text NOT NULL DEFAULT 'ACTIVE',
    created_by text NULL,
    last_used_at timestamptz NULL,
    expires_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS ix_api_keys_tenant_status
    ON api_keys(tenant_id,status,created_at DESC);

COMMIT;
