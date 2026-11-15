package exporter

import (
    "bytes"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/xuri/excelize/v2"
)

type Profile struct {
    Format string
    Columns []string
    HeaderAliases map[string]string
}

var supportedColumns=map[string]bool{
    "record_type":true,
    "source_record_id":true,
    "target_record_id":true,
    "status":true,
    "score":true,
    "matching_rule":true,
    "difference_amount":true,
    "exception_type":true,
    "severity":true,
    "message":true,
    "approved_by":true,
    "approved_at":true,
}

func ValidateProfile(format string,columns []string,aliases map[string]string) error {
    switch format { case "json","csv","xlsx": default: return fmt.Errorf("format must be json, csv or xlsx") }
    if len(columns)==0 { return fmt.Errorf("at least one output column is required") }
    if len(columns)>len(supportedColumns) { return fmt.Errorf("too many output columns") }
    seen:=map[string]bool{}
    for _,column:=range columns {
        if !supportedColumns[column] { return fmt.Errorf("unsupported output column %q",column) }
        if seen[column] { return fmt.Errorf("duplicate output column %q",column) }
        seen[column]=true
    }
    for column,header:=range aliases {
        if !supportedColumns[column] { return fmt.Errorf("header alias references unsupported column %q",column) }
        if strings.TrimSpace(header)=="" { return fmt.Errorf("header alias for %q cannot be empty",column) }
    }
    return nil
}

func BuildWithProfile(profile Profile,report Report)([]byte,string,string,error){
    if err:=ValidateProfile(profile.Format,profile.Columns,profile.HeaderAliases); err!=nil {
        return nil,"","",err
    }
    rows:=profileRows(report)
    switch profile.Format {
    case "json":
        selected:=make([]map[string]string,0,len(rows))
        for _,row:=range rows {
            out:=map[string]string{}
            for _,column:=range profile.Columns { out[column]=row[column] }
            selected=append(selected,out)
        }
        data,err:=json.MarshalIndent(map[string]any{"run":report.Run,"rows":selected},"","  ")
        return data,"application/json","json",err
    case "csv":
        data,err:=profileCSV(profile,rows)
        return data,"text/csv","csv",err
    case "xlsx":
        data,err:=profileXLSX(profile,rows)
        return data,"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","xlsx",err
    }
    return nil,"","",fmt.Errorf("unsupported format")
}

func profileRows(report Report) []map[string]string {
    rows:=make([]map[string]string,0,len(report.Matches)+len(report.Groups)+len(report.Exceptions))
    for _,m:=range report.Matches {
        row:=map[string]string{
            "record_type":"match","source_record_id":m.SourceRecordID,"target_record_id":m.TargetRecordID,
            "status":m.Status,"score":m.Score,"matching_rule":m.MatchingRule,
        }
        if m.DifferenceAmount!=nil { row["difference_amount"]=*m.DifferenceAmount }
        if m.ApprovedBy!=nil { row["approved_by"]=*m.ApprovedBy }
        if m.ApprovedAt!=nil { row["approved_at"]=m.ApprovedAt.Format("2006-01-02T15:04:05Z07:00") }
        rows=append(rows,row)
    }
    for _,g:=range report.Groups {
        rows=append(rows,map[string]string{
            "record_type":"group",
            "source_record_id":strings.Join(g.SourceDocumentIDs,";"),
            "target_record_id":strings.Join(g.TargetRecordIDs,";"),
            "status":g.Status,
            "score":g.Score,
            "matching_rule":g.GroupType,
            "difference_amount":g.DifferenceAmount,
        })
    }
    for _,e:=range report.Exceptions {
        row:=map[string]string{
            "record_type":"exception","source_record_id":e.SourceRecordID,"status":e.Status,
            "exception_type":e.ExceptionType,"severity":e.Severity,
        }
        if e.Message!=nil { row["message"]=*e.Message }
        rows=append(rows,row)
    }
    return rows
}

func profileHeader(profile Profile,column string) string {
    if header:=strings.TrimSpace(profile.HeaderAliases[column]); header!="" { return header }
    return column
}

func profileCSV(profile Profile,rows []map[string]string)([]byte,error){
    var buf bytes.Buffer
    writer:=csv.NewWriter(&buf)
    headers:=make([]string,len(profile.Columns))
    for i,column:=range profile.Columns { headers[i]=profileHeader(profile,column) }
    if err:=writer.Write(headers); err!=nil { return nil,err }
    for _,row:=range rows {
        values:=make([]string,len(profile.Columns))
        for i,column:=range profile.Columns { values[i]=row[column] }
        if err:=writer.Write(values); err!=nil { return nil,err }
    }
    writer.Flush()
    return buf.Bytes(),writer.Error()
}

func profileXLSX(profile Profile,rows []map[string]string)([]byte,error){
    file:=excelize.NewFile()
    defer file.Close()
    sheet:=file.GetSheetName(0)
    if sheet!="" { _=file.SetSheetName(sheet,"Reconciliation") } else { sheet="Reconciliation" }
    for columnIndex,column:=range profile.Columns {
        cell,_:=excelize.CoordinatesToCellName(columnIndex+1,1)
        _=file.SetCellValue("Reconciliation",cell,profileHeader(profile,column))
    }
    for rowIndex,row:=range rows {
        for columnIndex,column:=range profile.Columns {
            cell,_:=excelize.CoordinatesToCellName(columnIndex+1,rowIndex+2)
            _=file.SetCellValue("Reconciliation",cell,row[column])
        }
    }
    if len(profile.Columns)>0 {
        last,_:=excelize.ColumnNumberToName(len(profile.Columns))
        _=file.SetColWidth("Reconciliation","A",last,22)
    }
    buffer,err:=file.WriteToBuffer()
    if err!=nil { return nil,err }
    return buffer.Bytes(),nil
}
