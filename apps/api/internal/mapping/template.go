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
    "calculated_line_total":true,
}

var allowedTypes=map[string]bool{
    "string":true,"integer":true,"decimal":true,"date":true,"datetime":true,
    "currency":true,"percentage":true,"boolean":true,"identifier":true,
}

var allowedOperations=map[string]bool{
    // Keep this list aligned with the extraction runtime. Unsupported
    // operations are rejected rather than silently ignored.
    "normalize":true,
    "date":true,
    "decimal":true,
    "default":true,
    "exclude":true,
    "split":true,
    "combine":true,
    "compute":true,
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
            if op=="split" {
                normalizedConfig,err:=validateSplitConfig(operation.Config)
                if err!=nil {
                    return TemplateConfig{},fmt.Errorf(
                        "column %d operation %d split config: %w",
                        index,operationIndex,err)
                }
                config.Columns[index].Operations[operationIndex].Config=normalizedConfig
            }
            if op=="combine" {
                normalizedConfig,err:=validateCombineConfig(operation.Config)
                if err!=nil {
                    return TemplateConfig{},fmt.Errorf(
                        "column %d operation %d combine config: %w",
                        index,operationIndex,err)
                }
                primaryAliases:=map[string]bool{}
                for _,alias:=range aliases { primaryAliases[NormalizeHeader(alias)]=true }
                for _,alias:=range normalizedConfig["with"].([]string) {
                    if primaryAliases[NormalizeHeader(alias)] {
                        return TemplateConfig{},fmt.Errorf(
                            "column %d operation %d combine source %q duplicates primary source",
                            index,operationIndex,alias)
                    }
                }
                config.Columns[index].Operations[operationIndex].Config=normalizedConfig
            }
            if op=="compute" {
                if column.Type!="decimal" {
                    return TemplateConfig{},fmt.Errorf(
                        "column %d operation %d compute requires decimal type",
                        index,operationIndex)
                }
                normalizedConfig,err:=validateComputeConfig(operation.Config)
                if err!=nil {
                    return TemplateConfig{},fmt.Errorf(
                        "column %d operation %d compute config: %w",
                        index,operationIndex,err)
                }
                primaryAliases:=map[string]bool{}
                for _,alias:=range aliases { primaryAliases[NormalizeHeader(alias)]=true }
                operand:=normalizedConfig["operand"].(string)
                if primaryAliases[NormalizeHeader(operand)] {
                    return TemplateConfig{},fmt.Errorf(
                        "column %d operation %d compute operand %q duplicates primary source",
                        index,operationIndex,operand)
                }
                config.Columns[index].Operations[operationIndex].Config=normalizedConfig
            }
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


func validateSplitConfig(config map[string]any)(map[string]any,error){
    if config==nil { return nil,fmt.Errorf("config is required") }

    for key:=range config {
        switch key {
        case "delimiter","index","trim":
        default:
            return nil,fmt.Errorf("unsupported key %q",key)
        }
    }

    delimiter,ok:=config["delimiter"].(string)
    if !ok || delimiter=="" {
        return nil,fmt.Errorf("delimiter must be a non-empty string")
    }
    if len(delimiter)>16 {
        return nil,fmt.Errorf("delimiter must be at most 16 bytes")
    }

    rawIndex,ok:=config["index"]
    if !ok { return nil,fmt.Errorf("index is required") }

    index:=-1
    switch value:=rawIndex.(type) {
    case float64:
        if value>=0 && value<=31 && value==float64(int(value)) { index=int(value) }
    case int:
        if value>=0 && value<=31 { index=value }
    }
    if index<0 {
        return nil,fmt.Errorf("index must be an integer between 0 and 31")
    }

    trim:=true
    if rawTrim,exists:=config["trim"]; exists {
        parsed,ok:=rawTrim.(bool)
        if !ok { return nil,fmt.Errorf("trim must be boolean") }
        trim=parsed
    }

    return map[string]any{
        "delimiter":delimiter,
        "index":index,
        "trim":trim,
    },nil
}


func validateCombineConfig(config map[string]any)(map[string]any,error){
    if config==nil { return nil,fmt.Errorf("config is required") }
    for key:=range config {
        switch key {
        case "with","separator","skip_empty":
        default:
            return nil,fmt.Errorf("unsupported key %q",key)
        }
    }

    rawWith,ok:=config["with"]
    if !ok { return nil,fmt.Errorf("with is required") }

    aliases:=make([]string,0,8)
    seen:=map[string]bool{}
    appendAlias:=func(raw string) error {
        alias:=strings.TrimSpace(raw)
        normalized:=NormalizeHeader(alias)
        if normalized=="" { return fmt.Errorf("with aliases must be non-empty strings") }
        if len(alias)>200 { return fmt.Errorf("with alias must be at most 200 bytes") }
        if !seen[normalized] {
            seen[normalized]=true
            aliases=append(aliases,alias)
        }
        return nil
    }

    switch values:=rawWith.(type) {
    case []any:
        for _,value:=range values {
            alias,ok:=value.(string)
            if !ok { return nil,fmt.Errorf("with must contain only strings") }
            if err:=appendAlias(alias); err!=nil { return nil,err }
        }
    case []string:
        for _,alias:=range values {
            if err:=appendAlias(alias); err!=nil { return nil,err }
        }
    default:
        return nil,fmt.Errorf("with must be an array of strings")
    }
    if len(aliases)==0 || len(aliases)>8 {
        return nil,fmt.Errorf("with must contain between 1 and 8 unique aliases")
    }

    separator:=" "
    if rawSeparator,exists:=config["separator"]; exists {
        parsed,ok:=rawSeparator.(string)
        if !ok { return nil,fmt.Errorf("separator must be a string") }
        separator=parsed
    }
    if len(separator)>16 { return nil,fmt.Errorf("separator must be at most 16 bytes") }

    skipEmpty:=false
    if rawSkip,exists:=config["skip_empty"]; exists {
        parsed,ok:=rawSkip.(bool)
        if !ok { return nil,fmt.Errorf("skip_empty must be boolean") }
        skipEmpty=parsed
    }

    return map[string]any{
        "with":aliases,
        "separator":separator,
        "skip_empty":skipEmpty,
    },nil
}


func validateComputeConfig(config map[string]any)(map[string]any,error){
    if config==nil { return nil,fmt.Errorf("config is required") }
    for key:=range config {
        switch key {
        case "operator","operand","scale":
        default:
            return nil,fmt.Errorf("unsupported key %q",key)
        }
    }

    operator,ok:=config["operator"].(string)
    if !ok { return nil,fmt.Errorf("operator is required") }
    operator=strings.ToLower(strings.TrimSpace(operator))
    switch operator {
    case "add","subtract","multiply","divide":
    default:
        return nil,fmt.Errorf("operator must be add, subtract, multiply, or divide")
    }

    operand,ok:=config["operand"].(string)
    if !ok || NormalizeHeader(operand)=="" {
        return nil,fmt.Errorf("operand must be a non-empty source alias")
    }
    operand=strings.TrimSpace(operand)
    if len(operand)>200 { return nil,fmt.Errorf("operand must be at most 200 bytes") }

    rawScale,ok:=config["scale"]
    if !ok { return nil,fmt.Errorf("scale is required") }
    scale:=-1
    switch value:=rawScale.(type) {
    case float64:
        if value>=0 && value<=8 && value==float64(int(value)) { scale=int(value) }
    case int:
        if value>=0 && value<=8 { scale=value }
    }
    if scale<0 { return nil,fmt.Errorf("scale must be an integer between 0 and 8") }

    return map[string]any{
        "operator":operator,
        "operand":operand,
        "scale":scale,
    },nil
}
