package pipeline

import (
    "context"
    "encoding/json"
    "fmt"
    "regexp"
    "strings"
)

var supplierLookupNonAlphaNumeric=regexp.MustCompile(`[^A-Z0-9]+`)

type supplierLookupMatch struct {
    ID string
    SupplierName string
    SupplierCode string
}

func normalizeSupplierLookup(value string) string {
    return supplierLookupNonAlphaNumeric.ReplaceAllString(
        strings.ToUpper(strings.TrimSpace(value)),"")
}

func (w *Worker) exactSupplierLookup(
    ctx context.Context,
    tenantID,source string,
)([]supplierLookupMatch,error){
    normalized:=normalizeSupplierLookup(source)
    if normalized=="" { return []supplierLookupMatch{},nil }

    rows,err:=w.DB.Query(ctx,`
      WITH candidates AS (
        SELECT s.id,s.supplier_name,COALESCE(s.supplier_code,'') AS supplier_code,0 AS priority
        FROM suppliers s
        WHERE s.tenant_id=$1 AND s.status='ACTIVE' AND s.normalized_name=$2
        UNION ALL
        SELECT s.id,s.supplier_name,COALESCE(s.supplier_code,'') AS supplier_code,1 AS priority
        FROM supplier_aliases a
        JOIN suppliers s ON s.id=a.supplier_id AND s.tenant_id=a.tenant_id
        WHERE a.tenant_id=$1 AND s.status='ACTIVE' AND a.normalized_alias=$2
      ),
      deduped AS (
        SELECT id,supplier_name,supplier_code,MIN(priority) AS priority
        FROM candidates
        GROUP BY id,supplier_name,supplier_code
      )
      SELECT id::text,supplier_name,supplier_code
      FROM deduped
      ORDER BY priority,id
      LIMIT 2
    `,tenantID,normalized)
    if err!=nil { return nil,err }
    defer rows.Close()

    matches:=make([]supplierLookupMatch,0,2)
    for rows.Next(){
        var item supplierLookupMatch
        if err:=rows.Scan(&item.ID,&item.SupplierName,&item.SupplierCode); err!=nil {
            return nil,err
        }
        matches=append(matches,item)
    }
    return matches,rows.Err()
}

func (w *Worker) applyLookupTransforms(
    ctx context.Context,
    job JobMessage,
    canonicalBody []byte,
)([]byte,error){
    rawConfig,ok:=job.Options["mapping_config"].(map[string]any)
    if !ok { return canonicalBody,nil }
    rawColumns,ok:=rawConfig["columns"].([]any)
    if !ok { return canonicalBody,nil }

    var document map[string]any
    if err:=json.Unmarshal(canonicalBody,&document); err!=nil {
        return nil,fmt.Errorf("decode canonical for lookup: %w",err)
    }

    issues:=make([]map[string]any,0)
    changed:=false
    for _,rawColumn:=range rawColumns {
        column,ok:=rawColumn.(map[string]any)
        if !ok { continue }
        target,_:=column["target"].(string)
        if strings.TrimSpace(target)!="supplier_id" { continue }

        rawOperations,ok:=column["operations"].([]any)
        if !ok { continue }
        for _,rawOperation:=range rawOperations {
            operation,ok:=rawOperation.(map[string]any)
            if !ok { continue }
            name,_:=operation["operation"].(string)
            if !strings.EqualFold(strings.TrimSpace(name),"lookup") { continue }

            config,_:=operation["config"].(map[string]any)
            registry,_:=config["registry"].(string)
            sourceField,_:=config["source_field"].(string)
            onMissing,_:=config["on_missing"].(string)
            registry=strings.ToLower(strings.TrimSpace(registry))
            sourceField=strings.ToLower(strings.TrimSpace(sourceField))
            onMissing=strings.ToLower(strings.TrimSpace(onMissing))
            if onMissing=="" { onMissing="review" }
            if registry!="supplier_master" || sourceField!="supplier_name" {
                return nil,fmt.Errorf("invalid lookup runtime contract")
            }

            source,ok:=document[sourceField].(map[string]any)
            sourceValue:=""
            if ok {
                sourceValue,_=source["value"].(string)
                sourceValue=strings.TrimSpace(sourceValue)
            }
            if sourceValue=="" {
                if onMissing=="review" {
                    issues=append(issues,map[string]any{
                        "target":target,"code":"LOOKUP_SOURCE_MISSING","source_field":sourceField,
                    })
                }
                continue
            }

            matches,err:=w.exactSupplierLookup(ctx,job.TenantID,sourceValue)
            if err!=nil { return nil,fmt.Errorf("supplier lookup: %w",err) }
            if len(matches)==0 {
                if onMissing=="review" {
                    issues=append(issues,map[string]any{
                        "target":target,"code":"SUPPLIER_NOT_FOUND","source_value":sourceValue,
                    })
                }
                continue
            }
            if len(matches)>1 {
                issues=append(issues,map[string]any{
                    "target":target,"code":"SUPPLIER_LOOKUP_AMBIGUOUS","source_value":sourceValue,
                })
                continue
            }

            evidence:=map[string]any{}
            if sourceEvidence,ok:=source["evidence"].(map[string]any); ok {
                for key,value:=range sourceEvidence { evidence[key]=value }
            }
            evidence["method"]="supplier_master_lookup"
            evidence["confidence"]=1.0
            evidence["registry"]="supplier_master"
            evidence["matched_supplier_name"]=matches[0].SupplierName

            document[target]=map[string]any{
                "raw":sourceValue,
                "value":matches[0].ID,
                "data_type":"identifier",
                "evidence":evidence,
            }
            changed=true
        }
    }

    if len(issues)>0 {
        document["requires_review"]=true
        document["lookup_issues"]=issues
        changed=true
    }
    if !changed { return canonicalBody,nil }

    result,err:=json.MarshalIndent(document,"","  ")
    if err!=nil { return nil,fmt.Errorf("encode canonical lookup result: %w",err) }
    return result,nil
}
