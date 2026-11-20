package store

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/jahla2/rpd-docs/apps/api/internal/reconciliation"
)

type Supplier struct {
    ID string `json:"id"`
    SupplierCode *string `json:"supplier_code,omitempty"`
    SupplierName string `json:"supplier_name"`
    TaxID *string `json:"tax_id,omitempty"`
    NormalizedName *string `json:"normalized_name,omitempty"`
    Status string `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type SupplierAlias struct {
    ID string `json:"id"`
    SupplierID string `json:"supplier_id"`
    Alias string `json:"alias"`
    NormalizedAlias string `json:"normalized_alias"`
    CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateSupplier(ctx context.Context,tenantID,name,code,taxID string)(Supplier,error){
    normalized:=reconciliation.NormalizeReference(name)
    if normalized=="" { return Supplier{},fmt.Errorf("supplier name is required") }
    var supplier Supplier
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO suppliers
        (tenant_id,supplier_code,supplier_name,tax_id,normalized_name,status)
      VALUES ($1,NULLIF($2,''),$3,NULLIF($4,''),$5,'ACTIVE')
      RETURNING id::text,supplier_code,supplier_name,tax_id,normalized_name,status,created_at,updated_at
    `,tenantID,strings.TrimSpace(code),strings.TrimSpace(name),strings.TrimSpace(taxID),normalized).Scan(
      &supplier.ID,&supplier.SupplierCode,&supplier.SupplierName,&supplier.TaxID,&supplier.NormalizedName,
      &supplier.Status,&supplier.CreatedAt,&supplier.UpdatedAt)
    return supplier,err
}

func (s *Store) AddSupplierAlias(ctx context.Context,tenantID,supplierID,alias string)(SupplierAlias,error){
    normalized:=reconciliation.NormalizeReference(alias)
    if normalized=="" { return SupplierAlias{},fmt.Errorf("alias is required") }
    var result SupplierAlias
    err:=s.pool.QueryRow(ctx,`
      INSERT INTO supplier_aliases (tenant_id,supplier_id,alias,normalized_alias)
      VALUES ($1,$2,$3,$4)
      ON CONFLICT (tenant_id,normalized_alias)
      DO UPDATE SET supplier_id=EXCLUDED.supplier_id,alias=EXCLUDED.alias
      RETURNING id::text,supplier_id::text,alias,normalized_alias,created_at
    `,tenantID,supplierID,strings.TrimSpace(alias),normalized).Scan(
      &result.ID,&result.SupplierID,&result.Alias,&result.NormalizedAlias,&result.CreatedAt)
    return result,err
}

func (s *Store) SearchSuppliers(ctx context.Context,tenantID,query string,limit int)([]Supplier,error){
    if limit<=0 || limit>100 { limit=25 }
    query=strings.TrimSpace(query)
    rows,err:=s.pool.Query(ctx,`
      SELECT DISTINCT s.id::text,s.supplier_code,s.supplier_name,s.tax_id,s.normalized_name,
             s.status,s.created_at,s.updated_at
      FROM suppliers s
      LEFT JOIN supplier_aliases a ON a.supplier_id=s.id AND a.tenant_id=s.tenant_id
      WHERE s.tenant_id=$1
        AND (
          $2=''
          OR s.normalized_name=$3
          OR similarity(s.supplier_name,$2)>=0.35
          OR similarity(COALESCE(a.alias,''),$2)>=0.35
        )
      ORDER BY
        CASE WHEN s.normalized_name=$3 THEN 0 ELSE 1 END,
        greatest(similarity(s.supplier_name,$2),similarity(COALESCE(a.alias,''),$2)) DESC,
        s.supplier_name
      LIMIT $4
    `,tenantID,query,reconciliation.NormalizeReference(query),limit)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]Supplier,0)
    for rows.Next(){
        var supplier Supplier
        if err:=rows.Scan(&supplier.ID,&supplier.SupplierCode,&supplier.SupplierName,&supplier.TaxID,
          &supplier.NormalizedName,&supplier.Status,&supplier.CreatedAt,&supplier.UpdatedAt); err!=nil {
            return nil,err
        }
        out=append(out,supplier)
    }
    return out,rows.Err()
}

func (s *Store) ResolveSupplierKeys(ctx context.Context,tenantID,sourceName string,threshold float64)([]string,error){
    if threshold<=0 { threshold=0.72 }
    normalized:=reconciliation.NormalizeReference(sourceName)
    keys:=[]string{}
    rows,err:=s.pool.Query(ctx,`
      WITH resolved AS (
        SELECT s.normalized_name AS key, similarity(s.supplier_name,$2) AS score
        FROM suppliers s
        WHERE s.tenant_id=$1 AND s.status='ACTIVE'
        UNION ALL
        SELECT a.normalized_alias AS key, similarity(a.alias,$2) AS score
        FROM supplier_aliases a
        JOIN suppliers s ON s.id=a.supplier_id
        WHERE a.tenant_id=$1 AND s.status='ACTIVE'
        UNION ALL
        SELECT a.normalized_alias AS key, 1.0 AS score
        FROM supplier_aliases a
        JOIN suppliers s ON s.id=a.supplier_id
        WHERE a.tenant_id=$1 AND s.status='ACTIVE'
          AND s.normalized_name=$3
      )
      SELECT DISTINCT key
      FROM resolved
      WHERE key IS NOT NULL AND key<>'' AND (score >= $4 OR key=$3)
      LIMIT 50
    `,tenantID,sourceName,normalized,threshold)
    if err!=nil { return nil,err }
    defer rows.Close()
    seen:=map[string]bool{}
    if normalized!="" { keys=append(keys,normalized); seen[normalized]=true }
    for rows.Next(){
        var key string
        if err:=rows.Scan(&key); err!=nil { return nil,err }
        if !seen[key] { seen[key]=true; keys=append(keys,key) }
    }
    return keys,rows.Err()
}


type SupplierLookupMatch struct {
    ID string `json:"id"`
    SupplierCode *string `json:"supplier_code,omitempty"`
    SupplierName string `json:"supplier_name"`
}

func (s *Store) LookupSupplierExact(ctx context.Context,tenantID,source string)([]SupplierLookupMatch,error){
    normalized:=reconciliation.NormalizeReference(source)
    if normalized=="" { return []SupplierLookupMatch{},nil }

    rows,err:=s.pool.Query(ctx,`
      WITH candidates AS (
        SELECT s.id,s.supplier_code,s.supplier_name,0 AS priority
        FROM suppliers s
        WHERE s.tenant_id=$1 AND s.status='ACTIVE' AND s.normalized_name=$2
        UNION ALL
        SELECT s.id,s.supplier_code,s.supplier_name,1 AS priority
        FROM supplier_aliases a
        JOIN suppliers s ON s.id=a.supplier_id AND s.tenant_id=a.tenant_id
        WHERE a.tenant_id=$1 AND s.status='ACTIVE' AND a.normalized_alias=$2
      ),
      deduped AS (
        SELECT id,supplier_code,supplier_name,MIN(priority) AS priority
        FROM candidates
        GROUP BY id,supplier_code,supplier_name
      )
      SELECT id::text,supplier_code,supplier_name
      FROM deduped
      ORDER BY priority,id
      LIMIT 2
    `,tenantID,normalized)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]SupplierLookupMatch,0,2)
    for rows.Next(){
        var item SupplierLookupMatch
        if err:=rows.Scan(&item.ID,&item.SupplierCode,&item.SupplierName); err!=nil {
            return nil,err
        }
        out=append(out,item)
    }
    return out,rows.Err()
}
