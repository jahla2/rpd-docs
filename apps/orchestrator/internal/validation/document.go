package validation

import (
    "encoding/json"
    "fmt"
    "math/big"
    "strings"
)

type genericField struct {
    Value string `json:"value"`
}

type genericDocument struct {
    DocumentType string `json:"document_type"`
    Fields map[string]*genericField `json:"fields"`
    Tables map[string][]map[string]any `json:"tables"`
}

func ValidateDocument(data []byte,documentType,tolerance string)(Result,error){
    switch strings.TrimSpace(strings.ToLower(documentType)) {
    case "invoice":
        return ValidateInvoice(data,tolerance)
    case "supplier_statement","bank_statement":
        return validateStatement(data,documentType)
    default:
        return Result{},fmt.Errorf("unsupported document type for validation %q",documentType)
    }
}

func validateStatement(data []byte,documentType string)(Result,error){
    var doc genericDocument
    if err:=json.Unmarshal(data,&doc); err!=nil { return Result{},err }

    result:=Result{SchemaVersion:"1.0",Valid:true,Rules:make([]RuleResult,0)}
    required:=[]string{"closing_balance"}
    if documentType=="supplier_statement" {
        required=append([]string{"supplier_name"},required...)
    } else {
        required=append([]string{"account_number"},required...)
    }

    for _,name:=range required {
        value:=doc.Fields[name]
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

    for _,name:=range []string{"opening_balance","closing_balance"} {
        field:=doc.Fields[name]
        if field==nil || strings.TrimSpace(field.Value)=="" { continue }
        if _,ok:=new(big.Rat).SetString(strings.TrimSpace(field.Value)); !ok {
            result.add(RuleResult{
                Code:strings.ToUpper(name)+"_NUMERIC",
                Status:"FAIL",Severity:"ERROR",
                Message:name+" is not numeric",
            })
        }
    }

    if len(doc.Tables["transactions"])==0 {
        result.add(RuleResult{
            Code:"TRANSACTIONS_PRESENT",
            Status:"WARN",Severity:"WARNING",
            Message:"no statement transaction rows were extracted",
        })
    } else {
        result.add(RuleResult{
            Code:"TRANSACTIONS_PRESENT",
            Status:"PASS",Severity:"INFO",
            Message:"statement transaction rows were extracted",
            Evidence:map[string]any{"row_count":len(doc.Tables["transactions"])},
        })
    }

    return result,nil
}
