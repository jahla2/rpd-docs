package store

import (
    "context"
    "encoding/json"
    "time"
)

type OutputProfile struct {
    ID string `json:"id"`
    TenantID string `json:"tenant_id"`
    Name string `json:"name"`
    Format string `json:"format"`
    Columns []string `json:"columns"`
    HeaderAliases map[string]string `json:"header_aliases"`
    Active bool `json:"active"`
    CreatedBy *string `json:"created_by,omitempty"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) CreateOutputProfile(
    ctx context.Context,tenantID,name,format,createdBy string,columns []string,headerAliases map[string]string,
)(OutputProfile,error){
    columnsJSON,_:=json.Marshal(columns)
    aliasesJSON,_:=json.Marshal(headerAliases)
    var p OutputProfile
    var rawColumns,rawAliases []byte
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO output_profiles
        (tenant_id,name,format,columns_json,header_aliases_json,active,created_by)
      VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,true,NULLIF($6,''))
      RETURNING id::text,tenant_id::text,name,format,columns_json,header_aliases_json,
                active,created_by,created_at,updated_at
    `,tenantID,name,format,string(columnsJSON),string(aliasesJSON),createdBy).Scan(
      &p.ID,&p.TenantID,&p.Name,&p.Format,&rawColumns,&rawAliases,&p.Active,&p.CreatedBy,&p.CreatedAt,&p.UpdatedAt)
    if err!=nil { return OutputProfile{},err }
    _=json.Unmarshal(rawColumns,&p.Columns)
    _=json.Unmarshal(rawAliases,&p.HeaderAliases)
    return p,nil
}

func (s *Store) GetOutputProfile(ctx context.Context,tenantID,id string)(OutputProfile,error){
    var p OutputProfile
    var rawColumns,rawAliases []byte
    err:=s.pool.QueryRow(ctx,`
      SELECT id::text,tenant_id::text,name,format,columns_json,header_aliases_json,
             active,created_by,created_at,updated_at
      FROM output_profiles WHERE tenant_id=$1 AND id=$2
    `,tenantID,id).Scan(
      &p.ID,&p.TenantID,&p.Name,&p.Format,&rawColumns,&rawAliases,&p.Active,&p.CreatedBy,&p.CreatedAt,&p.UpdatedAt)
    if err!=nil { return OutputProfile{},err }
    _=json.Unmarshal(rawColumns,&p.Columns)
    _=json.Unmarshal(rawAliases,&p.HeaderAliases)
    return p,nil
}

func (s *Store) ListOutputProfiles(ctx context.Context,tenantID string)([]OutputProfile,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT id::text,tenant_id::text,name,format,columns_json,header_aliases_json,
             active,created_by,created_at,updated_at
      FROM output_profiles WHERE tenant_id=$1 ORDER BY active DESC,name
    `,tenantID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]OutputProfile,0)
    for rows.Next(){
        var p OutputProfile
        var rawColumns,rawAliases []byte
        if err:=rows.Scan(&p.ID,&p.TenantID,&p.Name,&p.Format,&rawColumns,&rawAliases,
          &p.Active,&p.CreatedBy,&p.CreatedAt,&p.UpdatedAt); err!=nil { return nil,err }
        _=json.Unmarshal(rawColumns,&p.Columns)
        _=json.Unmarshal(rawAliases,&p.HeaderAliases)
        out=append(out,p)
    }
    return out,rows.Err()
}
