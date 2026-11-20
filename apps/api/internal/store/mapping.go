package store

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
)

type MappingTemplate struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    Name string `json:"name"`
    DocumentType string `json:"document_type"`
    DocumentTypeID string `json:"document_type_id"`
    Version int `json:"version"`
    Status string `json:"status"`
    Config json.RawMessage `json:"config"`
    CreatedBy *string `json:"created_by,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    ActivatedAt *time.Time `json:"activated_at,omitempty"`
}

func (s *Store) ensureDocumentType(ctx context.Context,tenantID,code string)(string,error){
    var id string
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO document_types (tenant_id,code,name,schema_version,active)
      VALUES ($1,$2,initcap(replace($2,'_',' ')),1,true)
      ON CONFLICT (tenant_id,code,schema_version)
      DO UPDATE SET active=true
      RETURNING id::text
    `,tenantID,code).Scan(&id)
    return id,err
}

func (s *Store) CreateMappingTemplate(
    ctx context.Context,
    tenantID,name,documentType,createdBy string,
    config json.RawMessage,
)(MappingTemplate,error){
    documentTypeID,err:=s.ensureDocumentType(ctx,tenantID,documentType)
    if err!=nil { return MappingTemplate{},err }

    var t MappingTemplate
    err=s.pool.QueryRow(ctx,`
      INSERT INTO mapping_templates
        (tenant_id,name,document_type_id,version,status,config_json,created_by)
      VALUES ($1,$2,$3,1,'DRAFT',$4,$5)
      RETURNING id::text,tenant_id::text,name,$6,document_type_id::text,version,status,
                config_json,created_by,created_at,updated_at,activated_at
    `,tenantID,name,documentTypeID,config,nullIfEmpty(createdBy),documentType).Scan(
      &t.ID,&t.TenantID,&t.Name,&t.DocumentType,&t.DocumentTypeID,&t.Version,&t.Status,
      &t.Config,&t.CreatedBy,&t.CreatedAt,&t.UpdatedAt,&t.ActivatedAt)
    return t,err
}

func (s *Store) GetMappingTemplate(ctx context.Context,tenantID,id string)(MappingTemplate,error){
    var t MappingTemplate
    err:=s.pool.QueryRow(ctx,`
      SELECT mt.id::text,mt.tenant_id::text,mt.name,dt.code,mt.document_type_id::text,
             mt.version,mt.status,mt.config_json,mt.created_by,mt.created_at,mt.updated_at,mt.activated_at
      FROM mapping_templates mt
      JOIN document_types dt ON dt.id=mt.document_type_id
      WHERE mt.tenant_id=$1 AND mt.id=$2
    `,tenantID,id).Scan(
      &t.ID,&t.TenantID,&t.Name,&t.DocumentType,&t.DocumentTypeID,&t.Version,&t.Status,
      &t.Config,&t.CreatedBy,&t.CreatedAt,&t.UpdatedAt,&t.ActivatedAt)
    return t,err
}

func (s *Store) ListMappingTemplates(ctx context.Context,tenantID,documentType,status string)([]MappingTemplate,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT mt.id::text,mt.tenant_id::text,mt.name,dt.code,mt.document_type_id::text,
             mt.version,mt.status,mt.config_json,mt.created_by,mt.created_at,mt.updated_at,mt.activated_at
      FROM mapping_templates mt
      JOIN document_types dt ON dt.id=mt.document_type_id
      WHERE mt.tenant_id=$1
        AND ($2='' OR dt.code=$2)
        AND ($3='' OR mt.status=$3)
      ORDER BY mt.name,mt.version DESC
    `,tenantID,documentType,status)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]MappingTemplate,0)
    for rows.Next(){
        var t MappingTemplate
        if err:=rows.Scan(
          &t.ID,&t.TenantID,&t.Name,&t.DocumentType,&t.DocumentTypeID,&t.Version,&t.Status,
          &t.Config,&t.CreatedBy,&t.CreatedAt,&t.UpdatedAt,&t.ActivatedAt); err!=nil {
            return nil,err
        }
        out=append(out,t)
    }
    return out,rows.Err()
}

func (s *Store) CreateMappingTemplateVersion(
    ctx context.Context,
    tenantID,baseID,createdBy string,
    config json.RawMessage,
)(MappingTemplate,error){
    base,err:=s.GetMappingTemplate(ctx,tenantID,baseID)
    if err!=nil { return MappingTemplate{},err }

    var t MappingTemplate
    err=s.pool.QueryRow(ctx,`
      INSERT INTO mapping_templates
        (tenant_id,name,document_type_id,version,status,config_json,created_by)
      SELECT $1,$2,$3,COALESCE(MAX(version),0)+1,'DRAFT',$4,$5
      FROM mapping_templates
      WHERE tenant_id=$1 AND name=$2 AND document_type_id=$3
      RETURNING id::text,tenant_id::text,name,$6,document_type_id::text,version,status,
                config_json,created_by,created_at,updated_at,activated_at
    `,tenantID,base.Name,base.DocumentTypeID,config,nullIfEmpty(createdBy),base.DocumentType).Scan(
      &t.ID,&t.TenantID,&t.Name,&t.DocumentType,&t.DocumentTypeID,&t.Version,&t.Status,
      &t.Config,&t.CreatedBy,&t.CreatedAt,&t.UpdatedAt,&t.ActivatedAt)
    return t,err
}

func (s *Store) ActivateMappingTemplate(ctx context.Context,tenantID,id string)(MappingTemplate,error){
    tx,err:=s.pool.Begin(ctx)
    if err!=nil { return MappingTemplate{},err }
    defer tx.Rollback(ctx)

    var name,documentTypeID string
    err=tx.QueryRow(ctx,`
      SELECT name,document_type_id::text
      FROM mapping_templates
      WHERE tenant_id=$1 AND id=$2
      FOR UPDATE
    `,tenantID,id).Scan(&name,&documentTypeID)
    if err!=nil { return MappingTemplate{},err }

    if _,err=tx.Exec(ctx,`
      UPDATE mapping_templates
      SET status='ARCHIVED',updated_at=now()
      WHERE tenant_id=$1 AND name=$2 AND document_type_id=$3 AND status='ACTIVE' AND id<>$4
    `,tenantID,name,documentTypeID,id); err!=nil { return MappingTemplate{},err }

    if _,err=tx.Exec(ctx,`
      UPDATE mapping_templates
      SET status='ACTIVE',activated_at=now(),updated_at=now()
      WHERE tenant_id=$1 AND id=$2
    `,tenantID,id); err!=nil { return MappingTemplate{},err }

    if err=tx.Commit(ctx); err!=nil { return MappingTemplate{},err }
    return s.GetMappingTemplate(ctx,tenantID,id)
}

func (s *Store) ActiveMappingTemplateByName(ctx context.Context,tenantID,name,documentType string)(MappingTemplate,error){
    var t MappingTemplate
    err:=s.pool.QueryRow(ctx,`
      SELECT mt.id::text,mt.tenant_id::text,mt.name,dt.code,mt.document_type_id::text,
             mt.version,mt.status,mt.config_json,mt.created_by,mt.created_at,mt.updated_at,mt.activated_at
      FROM mapping_templates mt
      JOIN document_types dt ON dt.id=mt.document_type_id
      WHERE mt.tenant_id=$1 AND mt.name=$2 AND dt.code=$3 AND mt.status='ACTIVE'
    `,tenantID,name,documentType).Scan(
      &t.ID,&t.TenantID,&t.Name,&t.DocumentType,&t.DocumentTypeID,&t.Version,&t.Status,
      &t.Config,&t.CreatedBy,&t.CreatedAt,&t.UpdatedAt,&t.ActivatedAt)
    return t,err
}

var _ = fmt.Sprintf
