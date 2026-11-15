package mapping

import (
    "encoding/json"
    "fmt"
    "strings"
)

type Transform struct {
    Operation string `json:"operation"`
    Config map[string]any `json:"config,omitempty"`
}

type ColumnRule struct {
    SourceAliases []string `json:"source_aliases"`
    Target string `json:"target"`
    Type string `json:"type"`
    Required bool `json:"required,omitempty"`
    Operations []Transform `json:"operations,omitempty"`
}

type OutputConfig struct {
    Format string `json:"format,omitempty"`
    Columns []string `json:"columns,omitempty"`
}

type TemplateConfig struct {
    HeaderRow any `json:"header_row,omitempty"`
    Columns []ColumnRule `json:"columns"`
    Output OutputConfig `json:"output,omitempty"`
}

var allowedTargets=map[string]bool{
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
    "description":true,
    "item_code":true,
    "quantity":true,
    "unit":true,
    "unit_price":true,
    "line_total":true,
}

var allowedTypes=map[string]bool{
    "string":true,"integer":true,"decimal":true,"date":true,"datetime":true,
    "currency":true,"percentage":true,"boolean":true,"identifier":true,
}

var allowedOperations=map[string]bool{
    "rename":true,"normalize":true,"date":true,"decimal":true,"split":true,
    "combine":true,"compute":true,"lookup":true,"default":true,"exclude":true,
}

func ValidateTemplateConfig(raw json.RawMessage)(TemplateConfig,error){
    var config TemplateConfig
    decoderErr:=json.Unmarshal(raw,&config)
    if decoderErr!=nil { return TemplateConfig{},fmt.Errorf("invalid config_json: %w",decoderErr) }
    if len(config.Columns)==0 { return TemplateConfig{},fmt.Errorf("at least one column rule is required") }
    if len(config.Columns)>200 { return TemplateConfig{},fmt.Errorf("too many column rules") }

    usedTargets:=map[string]bool{}
    for index,column:=range config.Columns {
        column.Target=strings.TrimSpace(column.Target)
        column.Type=strings.ToLower(strings.TrimSpace(column.Type))
        if !allowedTargets[column.Target] {
            return TemplateConfig{},fmt.Errorf("column %d has unsupported target %q",index,column.Target)
        }
        if column.Type=="" { column.Type="string" }
        if !allowedTypes[column.Type] {
            return TemplateConfig{},fmt.Errorf("column %d has unsupported type %q",index,column.Type)
        }
        if usedTargets[column.Target] {
            return TemplateConfig{},fmt.Errorf("target %q is mapped more than once",column.Target)
        }
        usedTargets[column.Target]=true

        aliases:=make([]string,0,len(column.SourceAliases))
        seen:=map[string]bool{}
        for _,alias:=range column.SourceAliases {
            normalized:=NormalizeHeader(alias)
            if normalized!="" && !seen[normalized] {
                seen[normalized]=true
                aliases=append(aliases,strings.TrimSpace(alias))
            }
        }
        if len(aliases)==0 {
            return TemplateConfig{},fmt.Errorf("column %d requires at least one source alias",index)
        }
        config.Columns[index].SourceAliases=aliases
        config.Columns[index].Target=column.Target
        config.Columns[index].Type=column.Type

        for operationIndex,operation:=range column.Operations {
            op:=strings.ToLower(strings.TrimSpace(operation.Operation))
            if !allowedOperations[op] {
                return TemplateConfig{},fmt.Errorf(
                    "column %d operation %d has unsupported operation %q",
                    index,operationIndex,operation.Operation)
            }
            config.Columns[index].Operations[operationIndex].Operation=op
        }
    }

    if config.Output.Format!="" {
        format:=strings.ToLower(strings.TrimSpace(config.Output.Format))
        switch format {
        case "json","csv","xlsx":
            config.Output.Format=format
        default:
            return TemplateConfig{},fmt.Errorf("unsupported output format %q",config.Output.Format)
        }
    }
    for _,column:=range config.Output.Columns {
        if !allowedTargets[column] {
            return TemplateConfig{},fmt.Errorf("unsupported output column %q",column)
        }
    }
    return config,nil
}
