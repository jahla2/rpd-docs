package store

import (
    "context"
    "time"
)

type APIKeyAuth struct {
    ID string
    TenantID string
    Name string
    KeyPrefix string
    SecretHash string
    Role string
    Status string
    ExpiresAt *time.Time
    Permissions []string
}

type APIKeyInfo struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    Name string `json:"name"`
    KeyPrefix string `json:"key_prefix"`
    Role string `json:"role"`
    Status string `json:"status"`
    LastUsedAt *time.Time `json:"last_used_at,omitempty"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) GetAPIKeyAuthByPrefix(ctx context.Context,prefix string)(APIKeyAuth,error){
    var key APIKeyAuth
    err:=s.pool.QueryRow(ctx,`
      SELECT k.id::text,k.tenant_id::text,k.name,k.key_prefix,k.secret_hash,k.role_code,k.status,k.expires_at,
             COALESCE(array_agg(rp.permission_code ORDER BY rp.permission_code)
               FILTER (WHERE rp.permission_code IS NOT NULL),'{}'::text[])
      FROM api_keys k
      LEFT JOIN role_permissions rp ON rp.role_code=k.role_code
      WHERE k.key_prefix=$1
      GROUP BY k.id
    `,prefix).Scan(&key.ID,&key.TenantID,&key.Name,&key.KeyPrefix,&key.SecretHash,&key.Role,
      &key.Status,&key.ExpiresAt,&key.Permissions)
    return key,err
}

func (s *Store) TouchAPIKey(ctx context.Context,id string) {
    _,_=s.pool.Exec(ctx,`UPDATE api_keys SET last_used_at=now() WHERE id=$1`,id)
}

func (s *Store) CreateAPIKey(
    ctx context.Context,tenantID,name,prefix,secretHash,role,createdBy string,expiresAt *time.Time,
)(APIKeyInfo,error){
    var key APIKeyInfo
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO api_keys
        (tenant_id,name,key_prefix,secret_hash,role_code,status,created_by,expires_at)
      VALUES ($1,$2,$3,$4,$5,'ACTIVE',NULLIF($6,''),$7)
      RETURNING id::text,tenant_id::text,name,key_prefix,role_code,status,last_used_at,expires_at,created_at,revoked_at
    `,tenantID,name,prefix,secretHash,role,createdBy,expiresAt).Scan(
      &key.ID,&key.TenantID,&key.Name,&key.KeyPrefix,&key.Role,&key.Status,&key.LastUsedAt,
      &key.ExpiresAt,&key.CreatedAt,&key.RevokedAt)
    return key,err
}

func (s *Store) ListAPIKeys(ctx context.Context,tenantID string)([]APIKeyInfo,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,tenant_id::text,name,key_prefix,role_code,status,last_used_at,expires_at,created_at,revoked_at
      FROM api_keys WHERE tenant_id=$1 ORDER BY created_at DESC
    `,tenantID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]APIKeyInfo,0)
    for rows.Next(){
        var key APIKeyInfo
        if err:=rows.Scan(&key.ID,&key.TenantID,&key.Name,&key.KeyPrefix,&key.Role,&key.Status,
          &key.LastUsedAt,&key.ExpiresAt,&key.CreatedAt,&key.RevokedAt); err!=nil { return nil,err }
        out=append(out,key)
    }
    return out,rows.Err()
}

func (s *Store) RevokeAPIKey(ctx context.Context,tenantID,id string) error {
    _,err:=s.pool.Exec(ctx,`
      UPDATE api_keys SET status='REVOKED',revoked_at=now()
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,id)
    return err
}
