package erp

import (
    "bytes"
    "testing"

    "github.com/xuri/excelize/v2"
)

func TestXLSXImport(t *testing.T){
    f:=excelize.NewFile()
    sheet:=f.GetSheetName(0)
    headers:=[]string{"Vendor","Invoice No","Posting Date","Currency","Gross Amount"}
    values:=[]string{"ABC Trading","INV-100","2026-11-16","PHP","1250.50"}

    for i,value:=range headers {
        cell,_:=excelize.CoordinatesToCellName(i+1,1)
        if err:=f.SetCellValue(sheet,cell,value); err!=nil { t.Fatal(err) }
    }
    for i,value:=range values {
        cell,_:=excelize.CoordinatesToCellName(i+1,2)
        if err:=f.SetCellValue(sheet,cell,value); err!=nil { t.Fatal(err) }
    }

    buffer,err:=f.WriteToBuffer()
    if err!=nil { t.Fatal(err) }
    _=f.Close()

    result,err:=Parse("erp.xlsx",bytes.NewReader(buffer.Bytes()),nil)
    if err!=nil { t.Fatal(err) }
    if len(result.Errors)!=0 { t.Fatalf("errors=%#v",result.Errors) }
    if len(result.Rows)!=1 { t.Fatalf("rows=%d",len(result.Rows)) }
    row:=result.Rows[0]
    if row.NormalizedReference!="INV100" || row.NormalizedSupplier!="ABCTRADING" || row.Amount!="1250.5000" {
        t.Fatalf("row=%#v",row)
    }
}
