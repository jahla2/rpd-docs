package mapping

import (
    "regexp"
    "strconv"
    "strings"
    "time"
)

type ColumnProfile struct {
    Header string `json:"header"`
    TotalCount int `json:"total_count"`
    NonEmptyCount int `json:"non_empty_count"`
    EmptyCount int `json:"empty_count"`
    UniqueCount int `json:"unique_count"`
    UniqueRatio float64 `json:"unique_ratio"`
    NumericRatio float64 `json:"numeric_ratio"`
    DateRatio float64 `json:"date_ratio"`
    BooleanRatio float64 `json:"boolean_ratio"`
    IdentifierRatio float64 `json:"identifier_ratio"`
    InferredType string `json:"inferred_type"`
    Samples []string `json:"samples"`
}

var identifierShape=regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._/-]{2,}$")

func ProfileColumn(header string,values []string) ColumnProfile {
    profile:=ColumnProfile{Header:header,TotalCount:len(values),Samples:make([]string,0,5)}
    unique:=map[string]struct{}{}
    numeric,dateCount,boolean,identifier:=0,0,0,0
    nonEmptyValues:=make([]string,0,len(values))

    for _,raw:=range values {
        value:=strings.TrimSpace(raw)
        if value=="" { continue }
        profile.NonEmptyCount++
        nonEmptyValues=append(nonEmptyValues,value)
        unique[value]=struct{}{}
        if len(profile.Samples)<5 { profile.Samples=append(profile.Samples,value) }

        cleaned:=strings.NewReplacer(",","","₱","","$","","€","","PHP","","USD","","EUR","").Replace(value)
        if _,err:=strconv.ParseFloat(strings.TrimSpace(cleaned),64); err==nil { numeric++ }
        if isDate(value) { dateCount++ }
        if isBoolean(value) { boolean++ }
        if identifierShape.MatchString(value) && containsDigit(value) { identifier++ }
    }

    profile.EmptyCount=profile.TotalCount-profile.NonEmptyCount
    profile.UniqueCount=len(unique)
    if profile.NonEmptyCount>0 {
        denominator:=float64(profile.NonEmptyCount)
        profile.UniqueRatio=float64(profile.UniqueCount)/denominator
        profile.NumericRatio=float64(numeric)/denominator
        profile.DateRatio=float64(dateCount)/denominator
        profile.BooleanRatio=float64(boolean)/denominator
        profile.IdentifierRatio=float64(identifier)/denominator
    }
    profile.InferredType=inferProfileType(nonEmptyValues,profile)
    return profile
}

func ProfileColumns(headers []string,samples map[string][]string) []ColumnProfile {
    profiles:=make([]ColumnProfile,0,len(headers))
    for _,header:=range headers {
        profiles=append(profiles,ProfileColumn(header,samples[header]))
    }
    return profiles
}

func inferProfileType(values []string,p ColumnProfile) string {
    if len(values)==0 { return "unknown" }
    if p.DateRatio>=0.80 { return "date" }
    if p.BooleanRatio>=0.80 { return "boolean" }
    if p.NumericRatio>=0.80 { return "decimal" }
    if p.IdentifierRatio>=0.80 && p.UniqueRatio>=0.70 { return "identifier" }
    return "string"
}

func isDate(value string) bool {
    layouts:=[]string{"2006-01-02","02/01/2006","01/02/2006","02-01-2006","01-02-2006","02-Jan-2006","Jan 2, 2006","January 2, 2006"}
    for _,layout:=range layouts {
        if _,err:=time.Parse(layout,value); err==nil { return true }
    }
    return false
}

func isBoolean(value string) bool {
    switch strings.ToLower(strings.TrimSpace(value)) {
    case "true","false","yes","no","y","n","1","0":
        return true
    default:
        return false
    }
}

func containsDigit(value string) bool {
    for _,r:=range value { if r>='0' && r<='9' { return true } }
    return false
}
