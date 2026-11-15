package exporter

import (
    "bytes"
    "encoding/csv"
    "encoding/json"
    "fmt"
    "strconv"

    "github.com/jahla2/rpd-docs/apps/api/internal/store"
    "github.com/xuri/excelize/v2"
)

type Report struct {
    Run store.ReconciliationRun `json:"run"`
    Matches []store.ReconciliationMatch `json:"matches"`
    Exceptions []store.ReconciliationException `json:"exceptions"`
}

func Build(format string,report Report)([]byte,string,string,error){
    switch format {
    case "json":
        data,err:=json.MarshalIndent(report,"","  ")
        return data,"application/json","json",err
    case "csv":
        data,err:=buildCSV(report)
        return data,"text/csv","csv",err
    case "xlsx":
        data,err:=buildXLSX(report)
        return data,"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet","xlsx",err
    default:
        return nil,"","",fmt.Errorf("unsupported export format %q",format)
    }
}

func buildCSV(report Report)([]byte,error){
    var buf bytes.Buffer
    w:=csv.NewWriter(&buf)

    rows:=[][]string{
        {"record_type","source_record_id","target_record_id","status","score","matching_rule","difference_amount","exception_type","severity","message"},
    }
    for _,m:=range report.Matches {
        diff:=""
        if m.DifferenceAmount!=nil { diff=*m.DifferenceAmount }
        rows=append(rows,[]string{
            "match",m.SourceRecordID,m.TargetRecordID,m.Status,m.Score,m.MatchingRule,diff,"","","",
        })
    }
    for _,e:=range report.Exceptions {
        message:=""
        if e.Message!=nil { message=*e.Message }
        rows=append(rows,[]string{
            "exception",e.SourceRecordID,"",e.Status,"","","",e.ExceptionType,e.Severity,message,
        })
    }

    for _,row:=range rows {
        if err:=w.Write(row); err!=nil { return nil,err }
    }
    w.Flush()
    if err:=w.Error(); err!=nil { return nil,err }
    return buf.Bytes(),nil
}

func buildXLSX(report Report)([]byte,error){
    f:=excelize.NewFile()
    defer f.Close()

    defaultSheet:=f.GetSheetName(0)
    if defaultSheet!="" {
        _=f.SetSheetName(defaultSheet,"Matches")
    }
    _,_ = f.NewSheet("Exceptions")

    matchHeaders:=[]string{
        "Source Record","Target Record","Status","Score","Matching Rule","Difference Amount","Approved By","Approved At",
    }
    for col,value:=range matchHeaders {
        cell,_:=excelize.CoordinatesToCellName(col+1,1)
        _=f.SetCellValue("Matches",cell,value)
    }
    for rowIndex,m:=range report.Matches {
        row:=rowIndex+2
        approvedBy:=""
        if m.ApprovedBy!=nil { approvedBy=*m.ApprovedBy }
        approvedAt:=""
        if m.ApprovedAt!=nil { approvedAt=m.ApprovedAt.Format("2006-01-02T15:04:05Z07:00") }
        diff:=""
        if m.DifferenceAmount!=nil { diff=*m.DifferenceAmount }
        values:=[]any{m.SourceRecordID,m.TargetRecordID,m.Status,m.Score,m.MatchingRule,diff,approvedBy,approvedAt}
        for col,value:=range values {
            cell,_:=excelize.CoordinatesToCellName(col+1,row)
            _=f.SetCellValue("Matches",cell,value)
        }
    }

    exceptionHeaders:=[]string{"Source Record","Exception Type","Severity","Status","Message"}
    for col,value:=range exceptionHeaders {
        cell,_:=excelize.CoordinatesToCellName(col+1,1)
        _=f.SetCellValue("Exceptions",cell,value)
    }
    for rowIndex,e:=range report.Exceptions {
        row:=rowIndex+2
        message:=""
        if e.Message!=nil { message=*e.Message }
        values:=[]any{e.SourceRecordID,e.ExceptionType,e.Severity,e.Status,message}
        for col,value:=range values {
            cell,_:=excelize.CoordinatesToCellName(col+1,row)
            _=f.SetCellValue("Exceptions",cell,value)
        }
    }

    _=f.SetColWidth("Matches","A","H",20)
    _=f.SetColWidth("Exceptions","A","E",24)

    buf,err:=f.WriteToBuffer()
    if err!=nil { return nil,err }
    return buf.Bytes(),nil
}

var _ = strconv.Itoa
