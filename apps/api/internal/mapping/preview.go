package mapping

import (
    "fmt"
    "math/big"
    "strings"
    "time"
)

type TemplatePreviewSuggestion struct {
    Source     string  `json:"source"`
    Target     string  `json:"target,omitempty"`
    Confidence float64 `json:"confidence"`
    Method     string  `json:"method"`
}

type TemplatePreview struct {
    DetectedSourceColumns []string                    `json:"detected_source_columns"`
    Suggestions           []TemplatePreviewSuggestion `json:"suggestions"`
    SampleTransformedRows []map[string]string         `json:"sample_transformed_rows"`
    ValidationErrors      []string                    `json:"validation_errors"`
    Confidence            float64                     `json:"confidence"`
    Profiles              []ColumnProfile             `json:"profiles"`
}

func PreviewTemplate(config TemplateConfig, headers []string, samples map[string][]string) TemplatePreview {
    preview := TemplatePreview{
        DetectedSourceColumns: append([]string(nil), headers...),
        Suggestions:           make([]TemplatePreviewSuggestion, 0, len(headers)),
        SampleTransformedRows: make([]map[string]string, 0),
        ValidationErrors:      make([]string, 0),
        Profiles:              ProfileColumns(headers, samples),
    }

    matchedRules := make(map[int]int)
    for headerIndex, header := range headers {
        normalized := NormalizeHeader(header)
        suggestion := TemplatePreviewSuggestion{
            Source: header,
            Method: "unmapped",
        }

        for ruleIndex, rule := range config.Columns {
            matched := false
            for _, alias := range rule.SourceAliases {
                if NormalizeHeader(alias) == normalized && normalized != "" {
                    suggestion.Target = rule.Target
                    suggestion.Confidence = 1.0
                    suggestion.Method = "template_alias_exact"
                    if existingHeaderIndex, exists := matchedRules[ruleIndex]; exists {
                        preview.ValidationErrors = append(
                            preview.ValidationErrors,
                            fmt.Sprintf(
                                "target %q matches multiple source columns: %q and %q",
                                rule.Target, headers[existingHeaderIndex], header,
                            ),
                        )
                    } else {
                        matchedRules[ruleIndex] = headerIndex
                    }
                    matched = true
                    break
                }
            }
            if matched {
                break
            }
        }
        preview.Suggestions = append(preview.Suggestions, suggestion)
    }

    for ruleIndex, rule := range config.Columns {
        if rule.Required {
            if _, ok := matchedRules[ruleIndex]; !ok {
                preview.ValidationErrors = append(
                    preview.ValidationErrors,
                    fmt.Sprintf("required target %q has no matching source column", rule.Target),
                )
            }
        }
    }

    rowCount := 0
    for _, header := range headers {
        if count := len(samples[header]); count > rowCount {
            rowCount = count
        }
    }
    if rowCount > 20 {
        rowCount = 20
    }

    for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
        row := make(map[string]string)
        for ruleIndex, headerIndex := range matchedRules {
            rule := config.Columns[ruleIndex]
            header := headers[headerIndex]
            value := ""
            if values := samples[header]; rowIndex < len(values) {
                value = values[rowIndex]
            }
            value = strings.TrimSpace(value)
            transformed, excluded := ApplyOperations(value, rule.Operations)
            if excluded || transformed == nil {
                continue
            }
            row[rule.Target] = *transformed
        }
        preview.SampleTransformedRows = append(preview.SampleTransformedRows, row)
    }

    if len(config.Columns) == 0 {
        preview.Confidence = 0
    } else {
        preview.Confidence = float64(len(matchedRules)) / float64(len(config.Columns))
    }
    return preview
}

func ApplyOperations(value string, operations []Transform) (*string, bool) {
    current := value
    excluded := false

    for _, operation := range operations {
        name := strings.ToLower(strings.TrimSpace(operation.Operation))
        switch name {
        case "exclude":
            excluded = true
            return nil, excluded
        case "default":
            if strings.TrimSpace(current) == "" {
                if raw, ok := operation.Config["value"]; ok {
                    current = fmt.Sprint(raw)
                }
            }
        case "normalize":
            current = strings.Join(strings.Fields(current), " ")
            caseValue := strings.ToLower(strings.TrimSpace(fmt.Sprint(operation.Config["case"])))
            switch caseValue {
            case "upper":
                current = strings.ToUpper(current)
            case "lower":
                current = strings.ToLower(current)
            }
        case "decimal":
            normalized := normalizePreviewDecimal(current)
            if normalized == nil {
                return nil, false
            }
            current = *normalized
        case "date":
            normalized := normalizePreviewDate(current)
            if normalized == nil {
                return nil, false
            }
            current = *normalized
        case "split":
            delimiter,ok:=operation.Config["delimiter"].(string)
            if !ok || delimiter=="" { return nil,false }
            index,ok:=splitIndex(operation.Config["index"])
            if !ok { return nil,false }
            parts:=strings.Split(current,delimiter)
            if index<0 || index>=len(parts) { return nil,false }
            current=parts[index]
            trim:=true
            if rawTrim,exists:=operation.Config["trim"]; exists {
                parsed,ok:=rawTrim.(bool)
                if !ok { return nil,false }
                trim=parsed
            }
            if trim { current=strings.TrimSpace(current) }
        }
    }

    return &current, excluded
}

func normalizePreviewDecimal(value string) *string {
    cleaned := strings.TrimSpace(value)
    replacer := strings.NewReplacer(
        ",", "",
        "₱", "",
        "$", "",
        "€", "",
        "PHP", "",
        "USD", "",
        "EUR", "",
    )
    cleaned = strings.TrimSpace(replacer.Replace(cleaned))
    if cleaned == "" {
        return nil
    }
    if _, ok := new(big.Rat).SetString(cleaned); !ok {
        return nil
    }
    return &cleaned
}

func normalizePreviewDate(value string) *string {
    candidate := strings.ReplaceAll(strings.TrimSpace(value), ".", "/")
    layouts := []string{
        "2006-01-02",
        "02/01/2006",
        "01/02/2006",
        "02-01-2006",
        "01-02-2006",
        "02/01/06",
        "01/02/06",
        "02-Jan-2006",
        "Jan 2, 2006",
        "January 2, 2006",
    }
    for _, layout := range layouts {
        if parsed, err := time.Parse(layout, candidate); err == nil {
            normalized := parsed.Format("2006-01-02")
            return &normalized
        }
    }
    return nil
}


func splitIndex(raw any)(int,bool){
    switch value:=raw.(type) {
    case int:
        if value>=0 && value<=31 { return value,true }
    case float64:
        if value>=0 && value<=31 && value==float64(int(value)) { return int(value),true }
    }
    return 0,false
}
