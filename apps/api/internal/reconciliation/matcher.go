package reconciliation

import (
    "fmt"
    "math/big"
    "regexp"
    "strings"
    "time"
)

var nonAlphaNumeric=regexp.MustCompile(`[^A-Z0-9]+`)

type Record struct {
    ID string
    Supplier string
    Reference string
    Currency string
    Amount string
    Date time.Time
}

type Result struct {
    SourceID string `json:"source_id"`
    TargetID string `json:"target_id"`
    Status string `json:"status"`
    Score int `json:"score"`
    MatchingRule string `json:"matching_rule"`
    Difference string `json:"difference"`
    Evidence map[string]any `json:"evidence"`
}

func NormalizeReference(v string) string {
    return nonAlphaNumeric.ReplaceAllString(strings.ToUpper(strings.TrimSpace(v)),"")
}

func Match(source,target Record,absoluteTolerance string,dateWindowDays int) (Result,error) {
    similarity:=0.0
    if NormalizeReference(source.Supplier)==NormalizeReference(target.Supplier) { similarity=1.0 }
    return MatchAdvanced(source,target,absoluteTolerance,dateWindowDays,similarity)
}

func MatchAdvanced(source,target Record,absoluteTolerance string,dateWindowDays int,supplierSimilarity float64) (Result,error) {
    sourceAmount,ok:=new(big.Rat).SetString(source.Amount); if !ok { return Result{},fmt.Errorf("invalid source amount %q",source.Amount) }
    targetAmount,ok:=new(big.Rat).SetString(target.Amount); if !ok { return Result{},fmt.Errorf("invalid target amount %q",target.Amount) }
    tolerance,ok:=new(big.Rat).SetString(absoluteTolerance); if !ok { return Result{},fmt.Errorf("invalid tolerance %q",absoluteTolerance) }

    diff:=new(big.Rat).Sub(sourceAmount,targetAmount)
    if diff.Sign()<0 { diff.Neg(diff) }

    supplierEqual:=NormalizeReference(source.Supplier)==NormalizeReference(target.Supplier)
    if supplierEqual { supplierSimilarity=1.0 }
    if supplierSimilarity<0 { supplierSimilarity=0 }
    if supplierSimilarity>1 { supplierSimilarity=1 }
    referenceEqual:=NormalizeReference(source.Reference)==NormalizeReference(target.Reference)
    currencyEqual:=strings.EqualFold(strings.TrimSpace(source.Currency),strings.TrimSpace(target.Currency))
    amountExact:=diff.Sign()==0
    amountWithin:=diff.Cmp(tolerance)<=0

    dateWithin:=true
    days:=0
    if !source.Date.IsZero() && !target.Date.IsZero() {
        days=int(source.Date.Sub(target.Date).Hours()/24)
        if days<0 { days=-days }
        dateWithin=days<=dateWindowDays
    }

    score:=0
    if referenceEqual { score+=40 }
    supplierPoints:=0
    switch {
    case supplierEqual: supplierPoints=25
    case supplierSimilarity>=0.90: supplierPoints=23
    case supplierSimilarity>=0.80: supplierPoints=20
    case supplierSimilarity>=0.70: supplierPoints=15
    case supplierSimilarity>=0.60: supplierPoints=8
    }
    score+=supplierPoints
    if amountExact { score+=20 } else if amountWithin { score+=15 }
    if dateWithin { score+=10 }
    if currencyEqual { score+=5 }

    status:="UNMATCHED"
    rule:="none"
    switch {
    case referenceEqual && supplierEqual && amountExact && currencyEqual:
        status="MATCHED_EXACT"; rule="supplier_reference_amount_currency"
    case referenceEqual && supplierEqual && amountWithin && currencyEqual:
        status="MATCHED_TOLERANCE"; rule="supplier_reference_amount_tolerance"
    case score>=75:
        status="MATCHED_COMPOSITE"; rule="weighted_composite"
    case score>=60:
        status="SUGGESTED_MATCH"; rule="weighted_suggestion"
    }

    return Result{
        SourceID:source.ID,TargetID:target.ID,Status:status,Score:score,
        MatchingRule:rule,Difference:diff.FloatString(4),
        Evidence:map[string]any{
            "supplier_equal":supplierEqual,"supplier_similarity":supplierSimilarity,"supplier_points":supplierPoints,
            "reference_equal":referenceEqual,
            "currency_equal":currencyEqual,"amount_exact":amountExact,
            "amount_within_tolerance":amountWithin,"date_within_window":dateWithin,
            "date_difference_days":days,
        },
    },nil
}
