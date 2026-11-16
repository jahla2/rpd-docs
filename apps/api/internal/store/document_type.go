package store

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
)

var ErrDocumentTypeConflict = errors.New("document type already exists")

type DocumentType struct {
    ID            string    `json:"id"`
    TenantID      string    `json:"tenant_id"`
    Code          string    `json:"code"`
    Name          string    `json:"name"`
    SchemaVersion int       `json:"schema_version"`
    Active        bool      `json:"active"`
    CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) ListDocumentTypes(ctx context.Context, tenantID string) ([]DocumentType, error) {
    rows, err := s.pool.Query(ctx, `
      SELECT id::text,tenant_id::text,code,name,schema_version,active,created_at
      FROM document_types
      WHERE tenant_id=$1
      ORDER BY code,schema_version DESC
    `, tenantID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    items := make([]DocumentType, 0)
    for rows.Next() {
        var item DocumentType
        if err := rows.Scan(
            &item.ID,&item.TenantID,&item.Code,&item.Name,&item.SchemaVersion,&item.Active,&item.CreatedAt,
        ); err != nil {
            return nil, err
        }
        items = append(items, item)
    }
    return items, rows.Err()
}

func (s *Store) GetDocumentType(ctx context.Context, tenantID, id string) (DocumentType, error) {
    var item DocumentType
    err := s.pool.QueryRow(ctx, `
      SELECT id::text,tenant_id::text,code,name,schema_version,active,created_at
      FROM document_types
      WHERE tenant_id=$1 AND id=$2
    `, tenantID, id).Scan(
        &item.ID,&item.TenantID,&item.Code,&item.Name,&item.SchemaVersion,&item.Active,&item.CreatedAt,
    )
    return item, err
}

func (s *Store) CreateDocumentType(
    ctx context.Context,
    tenantID, code, name string,
    schemaVersion int,
    active bool,
    actor string,
) (DocumentType, error) {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return DocumentType{}, err
    }
    defer tx.Rollback(ctx)

    var item DocumentType
    err = tx.QueryRow(ctx, `
      INSERT INTO document_types (tenant_id,code,name,schema_version,active)
      VALUES ($1,$2,$3,$4,$5)
      ON CONFLICT (tenant_id,code,schema_version) DO NOTHING
      RETURNING id::text,tenant_id::text,code,name,schema_version,active,created_at
    `, tenantID, code, name, schemaVersion, active).Scan(
        &item.ID,&item.TenantID,&item.Code,&item.Name,&item.SchemaVersion,&item.Active,&item.CreatedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return DocumentType{}, ErrDocumentTypeConflict
    }
    if err != nil {
        return DocumentType{}, err
    }

    afterJSON, _ := json.Marshal(item)
    if _, err = tx.Exec(ctx, `
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,after_json)
      VALUES ($1,'USER',$2,'DOCUMENT_TYPE_CREATED','document_type',$3,$4::jsonb)
    `, tenantID, nullIfEmpty(actor), item.ID, afterJSON); err != nil {
        return DocumentType{}, err
    }

    if err := tx.Commit(ctx); err != nil {
        return DocumentType{}, err
    }
    return item, nil
}

func (s *Store) UpdateDocumentType(
    ctx context.Context,
    tenantID, id, code, name string,
    schemaVersion int,
    active bool,
    actor string,
) (DocumentType, error) {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return DocumentType{}, err
    }
    defer tx.Rollback(ctx)

    var before DocumentType
    err = tx.QueryRow(ctx, `
      SELECT id::text,tenant_id::text,code,name,schema_version,active,created_at
      FROM document_types
      WHERE tenant_id=$1 AND id=$2
      FOR UPDATE
    `, tenantID, id).Scan(
        &before.ID,&before.TenantID,&before.Code,&before.Name,&before.SchemaVersion,&before.Active,&before.CreatedAt,
    )
    if err != nil {
        return DocumentType{}, err
    }

    var item DocumentType
    err = tx.QueryRow(ctx, `
      UPDATE document_types
      SET code=$3,name=$4,schema_version=$5,active=$6
      WHERE tenant_id=$1 AND id=$2
      RETURNING id::text,tenant_id::text,code,name,schema_version,active,created_at
    `, tenantID, id, code, name, schemaVersion, active).Scan(
        &item.ID,&item.TenantID,&item.Code,&item.Name,&item.SchemaVersion,&item.Active,&item.CreatedAt,
    )
    if err != nil {
        return DocumentType{}, err
    }

    beforeJSON, _ := json.Marshal(before)
    afterJSON, _ := json.Marshal(item)
    if _, err = tx.Exec(ctx, `
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,before_json,after_json)
      VALUES ($1,'USER',$2,'DOCUMENT_TYPE_UPDATED','document_type',$3,$4::jsonb,$5::jsonb)
    `, tenantID, nullIfEmpty(actor), item.ID, beforeJSON, afterJSON); err != nil {
        return DocumentType{}, err
    }

    if err := tx.Commit(ctx); err != nil {
        return DocumentType{}, err
    }
    return item, nil
}

func IsDocumentTypeConflict(err error) bool {
    if errors.Is(err, ErrDocumentTypeConflict) {
        return true
    }
    var pgErr *pgconn.PgError
    return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
