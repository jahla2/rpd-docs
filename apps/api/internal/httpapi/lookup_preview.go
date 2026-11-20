package httpapi

import (
    "context"
    "fmt"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/mapping"
)

func (s *Server) resolveLookupPreview(
    ctx context.Context,
    tenantID string,
    config mapping.TemplateConfig,
    request MappingPreviewRequest,
    preview *mapping.TemplatePreview,
) error {
    for _,column:=range config.Columns {
        for _,operation:=range column.Operations {
            if operation.Operation!="lookup" { continue }

            sourceHeader:=""
            for _,header:=range request.Headers {
                normalized:=mapping.NormalizeHeader(header)
                for _,alias:=range column.SourceAliases {
                    if normalized!="" && normalized==mapping.NormalizeHeader(alias) {
                        sourceHeader=header
                        break
                    }
                }
                if sourceHeader!="" { break }
            }
            if sourceHeader=="" { continue }

            onMissing:="review"
            if raw,ok:=operation.Config["on_missing"].(string); ok {
                onMissing=strings.ToLower(strings.TrimSpace(raw))
            }

            for rowIndex:=range preview.SampleTransformedRows {
                value:=""
                if values:=request.Samples[sourceHeader]; rowIndex<len(values) {
                    value=strings.TrimSpace(values[rowIndex])
                }
                if value=="" {
                    if onMissing=="review" {
                        preview.ValidationErrors=append(
                            preview.ValidationErrors,
                            fmt.Sprintf("sample row %d target %q supplier lookup source is empty",rowIndex+1,column.Target),
                        )
                    }
                    continue
                }

                matches,err:=s.store.LookupSupplierExact(ctx,tenantID,value)
                if err!=nil { return err }
                switch len(matches) {
                case 0:
                    if onMissing=="review" {
                        preview.ValidationErrors=append(
                            preview.ValidationErrors,
                            fmt.Sprintf("sample row %d target %q supplier %q was not found",rowIndex+1,column.Target,value),
                        )
                    }
                case 1:
                    preview.SampleTransformedRows[rowIndex][column.Target]=matches[0].ID
                default:
                    preview.ValidationErrors=append(
                        preview.ValidationErrors,
                        fmt.Sprintf("sample row %d target %q supplier %q is ambiguous",rowIndex+1,column.Target,value),
                    )
                }
            }
        }
    }
    return nil
}
