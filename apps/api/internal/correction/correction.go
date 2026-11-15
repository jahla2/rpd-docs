package correction

import (
    "encoding/json"
    "fmt"
    "math/big"
    "strings"
)

type Request struct {
    FieldUpdates map[string]string `json:"field_updates,omitempty"`
    LineItems []map[string]any `json:"line_items,omitempty"`
    ReplaceLineItems bool `json:"replace_line_items,omitempty"`
    Reason string `json:"reason,omitempty"`
}

type ValidationRule struct {
    Code string `json:"code"`
    Status string `json:"status"`
    Severity string `json:"severity"`
    Message string `json:"message"`
    Evidence map[string]any `json:"evidence,omitempty"`
}

type ValidationResult struct {
    SchemaVersion string `json:"schema_version"`
    Valid bool `json:"valid"`
    RequiresReview bool `json:"requires_review"`
    Rules []ValidationRule `json:"rules"`
}

var allowedFields=map[string]bool{
    "supplier_name":true,
    "supplier_code":true,
    "invoice_number":true,
    "invoice_date":true,
    "due_date":true,
    "po_number":true,
    "currency":true,
    "subtotal":true,
    "tax":true,
    "grand_total":true,
}

func Apply(canonicalJSON []byte,request Request,actor string)([]byte,ValidationResult,error){
    var doc map[string]any
    if err:=json.Unmarshal(canonicalJSON,&doc); err!=nil {
        return nil,ValidationResult{},fmt.Errorf("decode canonical JSON: %w",err)
    }
    if actor=="" { actor="api-user" }

    for field,value:=range request.FieldUpdates {
        if !allowedFields[field] {
            return nil,ValidationResult{},fmt.Errorf("field %q cannot be corrected through this endpoint",field)
        }
        normalized:=strings.TrimSpace(value)
        doc[field]=map[string]any{
            "raw":value,
            "value":normalized,
            "evidence":map[string]any{
                "method":"human_correction",
                "confidence":1.0,
                "corrected_by":actor,
            },
        }
    }

    if request.ReplaceLineItems {
        if request.LineItems==nil { request.LineItems=[]map[string]any{} }
        for index,item:=range request.LineItems {
            item["row_number"]=index+1
            item["correction_method"]="human_correction"
            item["corrected_by"]=actor
        }
        doc["line_items"]=request.LineItems
    }

    validation:=Validate(doc,"0.01")
    doc["requires_review"]=validation.RequiresReview
    doc["human_correction"]=map[string]any{
        "corrected_by":actor,
        "reason":strings.TrimSpace(request.Reason),
    }

    out,err:=json.MarshalIndent(doc,"","  ")
    if err!=nil { return nil,ValidationResult{},err }
    return out,validation,nil
}

func Validate(doc map[string]any,toleranceValue string) ValidationResult {
    result:=ValidationResult{SchemaVersion:"1.0",Valid:true,Rules:[]ValidationRule{}}
    tolerance,ok:=new(big.Rat).SetString(toleranceValue)
    if !ok { tolerance=new(big.Rat).SetFrac64(1,100) }

    required:=[]string{"supplier_name","invoice_number","invoice_date","currency","grand_total"}
    for _,field:=range required {
        if fieldValue(doc[field])=="" {
            result.add(ValidationRule{
                Code:"REQUIRED_"+strings.ToUpper(field),Status:"FAIL",Severity:"ERROR",
                Message:field+" is required",
            })
        } else {
            result.add(ValidationRule{
                Code:"REQUIRED_"+strings.ToUpper(field),Status:"PASS",Severity:"INFO",
                Message:field+" is present",
            })
        }
    }

    subtotal,subtotalOK:=rat(fieldValue(doc["subtotal"]))
    tax,taxOK:=rat(fieldValue(doc["tax"]))
    grand,grandOK:=rat(fieldValue(doc["grand_total"]))

    rawItems,_:=doc["line_items"].([]any)
    if rawItems==nil {
        if typed,ok:=doc["line_items"].([]map[string]any); ok {
            rawItems=make([]any,len(typed))
            for i,item:=range typed { rawItems[i]=item }
        }
    }

    sum:=new(big.Rat)
    lineTotals:=0
    for index,raw:=range rawItems {
        item,ok:=raw.(map[string]any)
        if !ok { continue }
        quantity,qOK:=rat(text(item["quantity"]))
        unitPrice,pOK:=rat(text(item["unit_price"]))
        lineTotal,lOK:=rat(text(item["line_total"]))
        if lOK { sum.Add(sum,lineTotal); lineTotals++ }
        if qOK && pOK && lOK {
            expected:=new(big.Rat).Mul(quantity,unitPrice)
            diff:=absDiff(expected,lineTotal)
            if diff.Cmp(tolerance)>0 {
                result.add(ValidationRule{
                    Code:"LINE_TOTAL_MATH",Status:"FAIL",Severity:"ERROR",
                    Message:fmt.Sprintf("line %d quantity × unit_price does not equal line_total",index+1),
                    Evidence:map[string]any{"line":index+1,"difference":diff.FloatString(4)},
                })
            }
        }
    }

    if subtotalOK && lineTotals>0 {
        diff:=absDiff(sum,subtotal)
        if diff.Cmp(tolerance)>0 {
            result.add(ValidationRule{
                Code:"LINE_SUM_SUBTOTAL",Status:"FAIL",Severity:"ERROR",
                Message:"sum of line totals does not equal subtotal",
                Evidence:map[string]any{"line_sum":sum.FloatString(4),"subtotal":subtotal.FloatString(4)},
            })
        }
    }

    if subtotalOK && taxOK && grandOK {
        expected:=new(big.Rat).Add(new(big.Rat).Set(subtotal),tax)
        if diff:=absDiff(expected,grand); diff.Cmp(tolerance)>0 {
            result.add(ValidationRule{
                Code:"SUBTOTAL_TAX_GRAND_TOTAL",Status:"FAIL",Severity:"ERROR",
                Message:"subtotal plus tax does not equal grand_total",
                Evidence:map[string]any{"difference":diff.FloatString(4)},
            })
        }
    }

    return result
}

func (r *ValidationResult) add(rule ValidationRule){
    r.Rules=append(r.Rules,rule)
    if rule.Status=="FAIL" && rule.Severity=="ERROR" {
        r.Valid=false
        r.RequiresReview=true
    }
}

func fieldValue(raw any) string {
    field,ok:=raw.(map[string]any)
    if !ok { return "" }
    return strings.TrimSpace(text(field["value"]))
}

func text(raw any) string {
    switch value:=raw.(type) {
    case string: return strings.TrimSpace(value)
    case json.Number: return value.String()
    case float64: return fmt.Sprintf("%v",value)
    default: return ""
    }
}

func rat(value string)(*big.Rat,bool){
    value=strings.TrimSpace(value)
    if value=="" { return nil,false }
    parsed,ok:=new(big.Rat).SetString(value)
    return parsed,ok
}

func absDiff(a,b *big.Rat)*big.Rat{
    diff:=new(big.Rat).Sub(a,b)
    if diff.Sign()<0 { diff.Neg(diff) }
    return diff
}
