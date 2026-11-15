package erp

import (
    "crypto/sha256"
    "encoding/csv"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "path/filepath"
    "sort"
    "strconv"
    "strings"
    "time"

    "github.com/xuri/excelize/v2"
)

type CanonicalRow struct {
    SourceRow int               `json:"source_row"`
    ExternalRecordID string     `json:"external_record_id,omitempty"`
    RecordDate *time.Time       `json:"record_date,omitempty"`
    SupplierName string         `json:"supplier_name,omitempty"`
    NormalizedSupplier string   `json:"normalized_supplier,omitempty"`
    Reference string            `json:"reference,omitempty"`
    NormalizedReference string  `json:"normalized_reference,omitempty"`
    Currency string             `json:"currency,omitempty"`
    Amount string               `json:"amount,omitempty"`
    Raw map[string]string       `json:"raw"`
    RowHash string              `json:"row_hash"`
}

type ImportResult struct {
    Headers []string            `json:"headers"`
    Mapping map[string]string   `json:"mapping"`
    Rows []CanonicalRow         `json:"rows"`
    Errors []RowError           `json:"errors"`
}

type RowError struct {
    Row int    `json:"row"`
    Code string `json:"code"`
    Message string `json:"message"`
}

var aliases = map[string][]string{
    "external_record_id":{"id","record id","transaction id","erp id","row id"},
    "record_date":{"date","record date","transaction date","invoice date","posting date","document date"},
    "supplier_name":{"supplier","supplier name","vendor","vendor name","payee"},
    "reference":{"reference","invoice","invoice no","invoice number","inv ref","document no","document number"},
    "currency":{"currency","currency code","ccy"},
    "amount":{"amount","total","invoice total","gross amount","document amount","balance"},
}

func NormalizeHeader(v string) string {
    v = strings.ToLower(strings.TrimSpace(v))
    replacer := strings.NewReplacer("_"," ","-"," ","."," ","/"," ")
    v = replacer.Replace(v)
    return strings.Join(strings.Fields(v)," ")
}

func NormalizeKey(v string) string {
    v = strings.ToUpper(strings.TrimSpace(v))
    var b strings.Builder
    for _,r := range v {
        if (r>='A'&&r<='Z') || (r>='0'&&r<='9') {
            b.WriteRune(r)
        }
    }
    return b.String()
}

func DetectMapping(headers []string, overrides map[string]string) map[string]string {
    result:=map[string]string{}
    usedTargets:=map[string]bool{}

    for source,target:=range overrides {
        sourceNorm:=NormalizeHeader(source)
        if sourceNorm!="" && validTarget(target) {
            result[sourceNorm]=target
            usedTargets[target]=true
        }
    }

    targets:=make([]string,0,len(aliases))
    for target:=range aliases { targets=append(targets,target) }
    sort.Strings(targets)

    for _,header:=range headers {
        h:=NormalizeHeader(header)
        if h=="" { continue }
        if _,ok:=result[h]; ok { continue }

        for _,target:=range targets {
            if usedTargets[target] { continue }
            candidates:=append([]string{strings.ReplaceAll(target,"_"," ")},aliases[target]...)
            matched:=false
            for _,candidate:=range candidates {
                if h==NormalizeHeader(candidate) {
                    result[h]=target
                    usedTargets[target]=true
                    matched=true
                    break
                }
            }
            if matched { break }
        }
    }
    return result
}

func validTarget(v string) bool {
    _,ok:=aliases[v]
    return ok
}

func Parse(filename string, reader io.Reader, overrides map[string]string) (ImportResult,error) {
    ext:=strings.ToLower(filepath.Ext(filename))
    switch ext {
    case ".csv":
        return parseCSV(reader,overrides)
    case ".xlsx":
        return parseXLSX(reader,overrides)
    default:
        return ImportResult{},fmt.Errorf("unsupported ERP import format %q",ext)
    }
}

func parseCSV(reader io.Reader,overrides map[string]string)(ImportResult,error){
    r:=csv.NewReader(reader)
    r.FieldsPerRecord=-1
    r.TrimLeadingSpace=true

    headers,err:=r.Read()
    if err!=nil { return ImportResult{},fmt.Errorf("read CSV header: %w",err) }

    rows:=make([][]string,0)
    for {
        row,err:=r.Read()
        if err==io.EOF { break }
        if err!=nil { return ImportResult{},fmt.Errorf("read CSV row: %w",err) }
        rows=append(rows,row)
    }
    return buildResult(headers,rows,overrides),nil
}

func parseXLSX(reader io.Reader,overrides map[string]string)(ImportResult,error){
    f,err:=excelize.OpenReader(reader)
    if err!=nil { return ImportResult{},fmt.Errorf("open XLSX: %w",err) }
    defer f.Close()

    sheets:=f.GetSheetList()
    if len(sheets)==0 { return ImportResult{},fmt.Errorf("XLSX contains no worksheets") }

    rowsIter,err:=f.Rows(sheets[0])
    if err!=nil { return ImportResult{},fmt.Errorf("read XLSX sheet: %w",err) }
    defer rowsIter.Close()

    if !rowsIter.Next() { return ImportResult{},fmt.Errorf("XLSX sheet is empty") }
    headers,err:=rowsIter.Columns()
    if err!=nil { return ImportResult{},err }

    rows:=make([][]string,0)
    for rowsIter.Next() {
        cols,err:=rowsIter.Columns()
        if err!=nil { return ImportResult{},err }
        rows=append(rows,cols)
    }
    return buildResult(headers,rows,overrides),nil
}

func buildResult(headers []string,rows [][]string,overrides map[string]string) ImportResult {
    mapping:=DetectMapping(headers,overrides)
    out:=ImportResult{
        Headers:append([]string(nil),headers...),
        Mapping:mapping,
        Rows:make([]CanonicalRow,0,len(rows)),
        Errors:make([]RowError,0),
    }

    for index,row:=range rows {
        sourceRow:=index+2
        raw:=map[string]string{}
        canonical:=map[string]string{}

        for col,header:=range headers {
            value:=""
            if col<len(row) { value=strings.TrimSpace(row[col]) }
            raw[header]=value
            if target,ok:=mapping[NormalizeHeader(header)]; ok && value!="" {
                canonical[target]=value
            }
        }

        if allEmpty(raw) { continue }

        parsed,errs:=canonicalizeRow(sourceRow,raw,canonical)
        if len(errs)>0 {
            out.Errors=append(out.Errors,errs...)
            continue
        }
        out.Rows=append(out.Rows,parsed)
    }
    return out
}

func canonicalizeRow(rowNumber int,raw,values map[string]string)(CanonicalRow,[]RowError){
    errors:=make([]RowError,0)

    supplier:=strings.TrimSpace(values["supplier_name"])
    reference:=strings.TrimSpace(values["reference"])
    amount:=normalizeAmount(values["amount"])
    currency:=strings.ToUpper(strings.TrimSpace(values["currency"]))

    if reference=="" {
        errors=append(errors,RowError{Row:rowNumber,Code:"MISSING_REFERENCE",Message:"reference/invoice number is required"})
    }
    if amount=="" {
        errors=append(errors,RowError{Row:rowNumber,Code:"INVALID_AMOUNT",Message:"amount is required and must be numeric"})
    }

    var recordDate *time.Time
    if rawDate:=strings.TrimSpace(values["record_date"]); rawDate!="" {
        if parsed,ok:=parseDate(rawDate); ok {
            recordDate=&parsed
        } else {
            errors=append(errors,RowError{Row:rowNumber,Code:"INVALID_DATE",Message:"record date could not be parsed"})
        }
    }

    externalID:=strings.TrimSpace(values["external_record_id"])
    if externalID=="" {
        externalID=fmt.Sprintf("row-%d",rowNumber)
    }

    row:=CanonicalRow{
        SourceRow:rowNumber,
        ExternalRecordID:externalID,
        RecordDate:recordDate,
        SupplierName:supplier,
        NormalizedSupplier:NormalizeKey(supplier),
        Reference:reference,
        NormalizedReference:NormalizeKey(reference),
        Currency:currency,
        Amount:amount,
        Raw:raw,
    }
    row.RowHash=hashRow(row)
    return row,errors
}

func normalizeAmount(v string) string {
    cleaned:=strings.TrimSpace(v)
    if cleaned=="" { return "" }
    replacer:=strings.NewReplacer(",","","₱","","$","","€","","PHP","","USD","","EUR","")
    cleaned=strings.TrimSpace(replacer.Replace(cleaned))
    n,err:=strconv.ParseFloat(cleaned,64)
    if err!=nil { return "" }
    return strconv.FormatFloat(n,'f',4,64)
}

func parseDate(v string)(time.Time,bool){
    layouts:=[]string{
        "2006-01-02","02/01/2006","01/02/2006","02-01-2006","01-02-2006",
        "02-Jan-2006","Jan 2, 2006","January 2, 2006",
    }
    for _,layout:=range layouts {
        if t,err:=time.Parse(layout,strings.TrimSpace(v)); err==nil { return t,true }
    }
    return time.Time{},false
}

func allEmpty(values map[string]string) bool {
    for _,v:=range values { if strings.TrimSpace(v)!="" { return false } }
    return true
}

func hashRow(row CanonicalRow) string {
    payload:=map[string]any{
        "external_record_id":row.ExternalRecordID,
        "date":dateString(row.RecordDate),
        "supplier":row.NormalizedSupplier,
        "reference":row.NormalizedReference,
        "currency":row.Currency,
        "amount":row.Amount,
    }
    b,_:=json.Marshal(payload)
    sum:=sha256.Sum256(b)
    return hex.EncodeToString(sum[:])
}

func dateString(t *time.Time) string {
    if t==nil { return "" }
    return t.Format("2006-01-02")
}
