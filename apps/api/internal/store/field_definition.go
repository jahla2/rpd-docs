package store

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
)

var ErrFieldDefinitionConflict = errors.New("field definition already exists")
var ErrFieldDefinitionIdentityImmutable = errors.New("field definition document type and field name are immutable")
var ErrFieldDefinitionDocumentTypeInactive = errors.New("document type is inactive")

type FieldDefinition struct {
    ID               string    `json:"id"`
    TenantID         string    `json:"tenant_id"`
    DocumentTypeID   string    `json:"document_type_id"`
    DocumentTypeCode string    `json:"document_type"`
    FieldName        string    `json:"field_name"`
    DisplayName      string    `json:"display_name"`
    DataType         string    `json:"data_type"`
    Required         bool      `json:"required"`
    Repeating        bool      `json:"repeating"`
    CreatedAt        time.Time `json:"created_at"`
}

func (s *Store) ListFieldDefinitions(
    ctx context.Context,
    tenantID, documentTypeID string,
) ([]FieldDefinition, error) {
    rows, err := s.pool.Query(ctx, `
      SELECT fd.id::text,fd.tenant_id::text,fd.document_type_id::text,dt.code,
             fd.field_name,fd.display_name,fd.data_type,fd.required,fd.repeating,fd.created_at
      FROM field_definitions fd
      JOIN document_types dt ON dt.id=fd.document_type_id
      WHERE fd.tenant_id=$1
        AND ($2='' OR fd.document_type_id=$2::uuid)
      ORDER BY dt.code,fd.field_name
    `, tenantID, documentTypeID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    items := make([]FieldDefinition, 0)
    for rows.Next() {
        var item FieldDefinition
        if err := rows.Scan(
            &item.ID,&item.TenantID,&item.DocumentTypeID,&item.DocumentTypeCode,
            &item.FieldName,&item.DisplayName,&item.DataType,&item.Required,&item.Repeating,&item.CreatedAt,
        ); err != nil {
            return nil, err
        }
        items = append(items, item)
    }
    return items, rows.Err()
}

func (s *Store) CreateFieldDefinition(
    ctx context.Context,
    tenantID, documentTypeID, fieldName, displayName, dataType string,
    required, repeating bool,
    actor string,
) (FieldDefinition, error) {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return FieldDefinition{}, err
    }
    defer tx.Rollback(ctx)

    var documentTypeCode string
    var active bool
    err = tx.QueryRow(ctx, `
      SELECT code,active
      FROM document_types
      WHERE tenant_id=$1 AND id=$2
      FOR SHARE
    `, tenantID, documentTypeID).Scan(&documentTypeCode, &active)
    if err != nil {
        return FieldDefinition{}, err
    }
    if !active {
        return FieldDefinition{}, ErrFieldDefinitionDocumentTypeInactive
    }

    var item FieldDefinition
    err = tx.QueryRow(ctx, `
      INSERT INTO field_definitions
        (tenant_id,document_type_id,field_name,display_name,data_type,required,repeating)
      VALUES ($1,$2,$3,$4,$5,$6,$7)
      ON CONFLICT (document_type_id,field_name) DO NOTHING
      RETURNING id::text,tenant_id::text,document_type_id::text,$8,
                field_name,display_name,data_type,required,repeating,created_at
    `, tenantID, documentTypeID, fieldName, displayName, dataType, required, repeating, documentTypeCode).Scan(
        &item.ID,&item.TenantID,&item.DocumentTypeID,&item.DocumentTypeCode,
        &item.FieldName,&item.DisplayName,&item.DataType,&item.Required,&item.Repeating,&item.CreatedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return FieldDefinition{}, ErrFieldDefinitionConflict
    }
    if err != nil {
        return FieldDefinition{}, err
    }

    afterJSON, _ := json.Marshal(item)
    if _, err = tx.Exec(ctx, `
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,after_json)
      VALUES ($1,'USER',$2,'FIELD_DEFINITION_CREATED','field_definition',$3,$4::jsonb)
    `, tenantID, nullIfEmpty(actor), item.ID, afterJSON); err != nil {
        return FieldDefinition{}, err
    }

    if err := tx.Commit(ctx); err != nil {
        return FieldDefinition{}, err
    }
    return item, nil
}

func (s *Store) UpdateFieldDefinition(
    ctx context.Context,
    tenantID, id, documentTypeID, fieldName, displayName, dataType string,
    required, repeating bool,
    actor string,
) (FieldDefinition, error) {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return FieldDefinition{}, err
    }
    defer tx.Rollback(ctx)

    var before FieldDefinition
    err = tx.QueryRow(ctx, `
      SELECT fd.id::text,fd.tenant_id::text,fd.document_type_id::text,dt.code,
             fd.field_name,fd.display_name,fd.data_type,fd.required,fd.repeating,fd.created_at
      FROM field_definitions fd
      JOIN document_types dt ON dt.id=fd.document_type_id
      WHERE fd.tenant_id=$1 AND fd.id=$2
      FOR UPDATE OF fd
    `, tenantID, id).Scan(
        &before.ID,&before.TenantID,&before.DocumentTypeID,&before.DocumentTypeCode,
        &before.FieldName,&before.DisplayName,&before.DataType,&before.Required,&before.Repeating,&before.CreatedAt,
    )
    if err != nil {
        return FieldDefinition{}, err
    }
    if before.DocumentTypeID != documentTypeID || before.FieldName != fieldName {
        return FieldDefinition{}, ErrFieldDefinitionIdentityImmutable
    }

    var item FieldDefinition
    err = tx.QueryRow(ctx, `
      UPDATE field_definitions
      SET display_name=$3,data_type=$4,required=$5,repeating=$6
      WHERE tenant_id=$1 AND id=$2
      RETURNING id::text,tenant_id::text,document_type_id::text,$7,
                field_name,display_name,data_type,required,repeating,created_at
    `, tenantID, id, displayName, dataType, required, repeating, before.DocumentTypeCode).Scan(
        &item.ID,&item.TenantID,&item.DocumentTypeID,&item.DocumentTypeCode,
        &item.FieldName,&item.DisplayName,&item.DataType,&item.Required,&item.Repeating,&item.CreatedAt,
    )
    if err != nil {
        return FieldDefinition{}, err
    }

    beforeJSON, _ := json.Marshal(before)
    afterJSON, _ := json.Marshal(item)
    if _, err = tx.Exec(ctx, `
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,before_json,after_json)
      VALUES ($1,'USER',$2,'FIELD_DEFINITION_UPDATED','field_definition',$3,$4::jsonb,$5::jsonb)
    `, tenantID, nullIfEmpty(actor), item.ID, beforeJSON, afterJSON); err != nil {
        return FieldDefinition{}, err
    }

    if err := tx.Commit(ctx); err != nil {
        return FieldDefinition{}, err
    }
    return item, nil
}

func (s *Store) DeleteFieldDefinition(
    ctx context.Context,
    tenantID, id, actor string,
) error {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    var before FieldDefinition
    err = tx.QueryRow(ctx, `
      SELECT fd.id::text,fd.tenant_id::text,fd.document_type_id::text,dt.code,
             fd.field_name,fd.display_name,fd.data_type,fd.required,fd.repeating,fd.created_at
      FROM field_definitions fd
      JOIN document_types dt ON dt.id=fd.document_type_id
      WHERE fd.tenant_id=$1 AND fd.id=$2
      FOR UPDATE OF fd
    `, tenantID, id).Scan(
        &before.ID,&before.TenantID,&before.DocumentTypeID,&before.DocumentTypeCode,
        &before.FieldName,&before.DisplayName,&before.DataType,&before.Required,&before.Repeating,&before.CreatedAt,
    )
    if err != nil {
        return err
    }

    if _, err = tx.Exec(ctx, `
      DELETE FROM field_definitions
      WHERE tenant_id=$1 AND id=$2
    `, tenantID, id); err != nil {
        return err
    }

    beforeJSON, _ := json.Marshal(before)
    if _, err = tx.Exec(ctx, `
      INSERT INTO audit_events
        (tenant_id,actor_type,actor_id,event_type,entity_type,entity_id,before_json)
      VALUES ($1,'USER',$2,'FIELD_DEFINITION_DELETED','field_definition',$3,$4::jsonb)
    `, tenantID, nullIfEmpty(actor), id, beforeJSON); err != nil {
        return err
    }

    return tx.Commit(ctx)
}

func IsFieldDefinitionConflict(err error) bool {
    if errors.Is(err, ErrFieldDefinitionConflict) {
        return true
    }
    var pgErr *pgconn.PgError
    return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
