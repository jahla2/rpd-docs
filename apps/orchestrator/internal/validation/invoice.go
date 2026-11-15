package validation

import (
    "encoding/json"
    "fmt"
    "math/big"
    "strings"
)

type field struct {
    Value string `json:"value"`
}

type lineItem struct {
    Description string `json:"description"`
    Quantity string `json:"quantity"`
    UnitPrice string `json:"unit_price"`
    Tax string `json:"tax"`
    LineTotal string `json:"line_total"`
}

type invoice struct {
    SupplierName *field `json:"supplier_name"`
    InvoiceNumber *field `json:"invoice_number"`
    InvoiceDate *field `json:"invoice_date"`
    Currency *field `json:"currency"`
    Subtotal *field `json:"subtotal"`
    Tax *field `json:"tax"`
    GrandTotal *field `json:"grand_total"`
    LineItems []lineItem `json:"line_items"`
}

type RuleResult struct {
    Code string `json:"code"`
    Status string `json:"status"`
    Severity string `json:"severity"`
    Message string `json:"message"`
    Evidence map[string]any `json:"evidence,omitempty"`
}

type Result struct {
    SchemaVersion string `json:"schema_version"`
    Valid bool `json:"valid"`
    RequiresReview bool `json:"requires_review"`
    Rules []RuleResult `json:"rules"`
}

func ValidateInvoice(data []byte,tolerance string)(Result,error){
    var inv invoice
    if err:=json.Unmarshal(data,&inv); err!=nil { return Result{},err }

    tol,ok:=new(big.Rat).SetString(tolerance)
    if !ok || tol.Sign()<0 { return Result{},fmt.Errorf("invalid validation tolerance %q",tolerance) }

    result:=Result{SchemaVersion:"1.0",Valid:true,Rules:make([]RuleResult,0)}
    required:=map[string]*field{
        "supplier_name":inv.SupplierName,
        "invoice_number":inv.InvoiceNumber,
        "invoice_date":inv.InvoiceDate,
        "currency":inv.Currency,
        "grand_total":inv.GrandTotal,
    }

    for _,name:=range []string{"supplier_name","invoice_number","invoice_date","currency","grand_total"} {
        value:=required[name]
        if value==nil || strings.TrimSpace(value.Value)=="" {
            result.add(RuleResult{
                Code:"REQUIRED_"+strings.ToUpper(name),
                Status:"FAIL",Severity:"ERROR",
                Message:name+" is required",
            })
        } else {
            result.add(RuleResult{
                Code:"REQUIRED_"+strings.ToUpper(name),
                Status:"PASS",Severity:"INFO",
                Message:name+" is present",
            })
        }
    }

    grandTotal,grandOK:=parse(fieldValue(inv.GrandTotal))
    if inv.GrandTotal!=nil && !grandOK {
        result.add(RuleResult{Code:"GRAND_TOTAL_NUMERIC",Status:"FAIL",Severity:"ERROR",Message:"grand_total is not numeric"})
    }

    var lineSum big.Rat
    lineSum.SetInt64(0)
    validLineTotals:=0

    for index,item:=range inv.LineItems {
        lineTotal,lineOK:=parse(item.LineTotal)
        if lineOK {
            lineSum.Add(&lineSum,lineTotal)
            validLineTotals++
        }

        qty,qtyOK:=parse(item.Quantity)
        unitPrice,priceOK:=parse(item.UnitPrice)
        if qtyOK && priceOK && lineOK {
            expected:=new(big.Rat).Mul(qty,unitPrice)
            difference:=absDiff(expected,lineTotal)
            if difference.Cmp(tol)>0 {
                result.add(RuleResult{
                    Code:"LINE_TOTAL_MATH",Status:"FAIL",Severity:"ERROR",
                    Message:fmt.Sprintf("line %d quantity × unit_price does not equal line_total",index+1),
                    Evidence:map[string]any{
                        "line":index+1,
                        "quantity":item.Quantity,
                        "unit_price":item.UnitPrice,
                        "line_total":item.LineTotal,
                        "expected":expected.FloatString(4),
                        "difference":difference.FloatString(4),
                    },
                })
            }
        }
    }

    subtotal,subtotalOK:=parse(fieldValue(inv.Subtotal))
    if subtotalOK && validLineTotals>0 {
        difference:=absDiff(&lineSum,subtotal)
        if difference.Cmp(tol)>0 {
            result.add(RuleResult{
                Code:"LINE_SUM_SUBTOTAL",Status:"FAIL",Severity:"ERROR",
                Message:"sum of line totals does not equal subtotal",
                Evidence:map[string]any{
                    "line_sum":lineSum.FloatString(4),
                    "subtotal":subtotal.FloatString(4),
                    "difference":difference.FloatString(4),
                },
            })
        } else {
            result.add(RuleResult{
                Code:"LINE_SUM_SUBTOTAL",Status:"PASS",Severity:"INFO",
                Message:"sum of line totals matches subtotal",
            })
        }
    }

    tax,taxOK:=parse(fieldValue(inv.Tax))
    if subtotalOK && grandOK {
        expected:=new(big.Rat).Set(subtotal)
        if taxOK { expected.Add(expected,tax) }
        difference:=absDiff(expected,grandTotal)
        if difference.Cmp(tol)>0 {
            result.add(RuleResult{
                Code:"SUBTOTAL_TAX_GRAND_TOTAL",Status:"FAIL",Severity:"ERROR",
                Message:"subtotal plus tax does not equal grand_total",
                Evidence:map[string]any{
                    "subtotal":subtotal.FloatString(4),
                    "tax":fieldValue(inv.Tax),
                    "grand_total":grandTotal.FloatString(4),
                    "expected":expected.FloatString(4),
                    "difference":difference.FloatString(4),
                },
            })
        } else {
            result.add(RuleResult{
                Code:"SUBTOTAL_TAX_GRAND_TOTAL",Status:"PASS",Severity:"INFO",
                Message:"subtotal plus tax matches grand_total",
            })
        }
    }

    if len(inv.LineItems)==0 {
        result.add(RuleResult{
            Code:"LINE_ITEMS_PRESENT",Status:"WARN",Severity:"WARNING",
            Message:"no line items were extracted",
        })
    }

    return result,nil
}

func (r *Result) add(rule RuleResult){
    r.Rules=append(r.Rules,rule)
    if rule.Status=="FAIL" && rule.Severity=="ERROR" {
        r.Valid=false
        r.RequiresReview=true
    }
}

func fieldValue(value *field) string {
    if value==nil { return "" }
    return strings.TrimSpace(value.Value)
}

func parse(value string)(*big.Rat,bool){
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
