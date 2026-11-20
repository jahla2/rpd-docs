package mapping

import (
    "regexp"
    "sort"
    "strconv"
    "strings"
    "time"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

type Suggestion struct {
    Source string `json:"source"`
    Target string `json:"target"`
    Confidence float64 `json:"confidence"`
    Method string `json:"method"`
}

type Registry struct {
    aliases map[string][]string
}

func NewRegistry(aliases map[string][]string) *Registry {
    normalized:=make(map[string][]string,len(aliases))
    for target,values:=range aliases {
        items:=[]string{NormalizeHeader(target)}
        for _,v:=range values { items=append(items,NormalizeHeader(v)) }
        normalized[target]=dedupe(items)
    }
    return &Registry{aliases:normalized}
}

func DefaultInvoiceRegistry() *Registry {
    return NewRegistry(map[string][]string{
        "invoice_number":{"invoice no","invoice #","inv no","inv ref","reference"},
        "description":{"item desc","description","particulars","product","item"},
        "quantity":{"qty","quantity","units"},
        "unit_price":{"unit cost","price each","unit rate","price"},
        "line_total":{"total","extended amount","line amount","amount"},
        "invoice_date":{"invoice date","date"},
        "po_number":{"po","po no","purchase order","purchase order no"},
    })
}

func NormalizeHeader(v string) string {
    v=strings.ToLower(strings.TrimSpace(v))
    v=nonWord.ReplaceAllString(v," ")
    return strings.Join(strings.Fields(v)," ")
}

func (r *Registry) Suggest(headers []string) []Suggestion {
    out:=make([]Suggestion,0,len(headers))
    for _,header:=range headers {
        n:=NormalizeHeader(header)
        best:=Suggestion{Source:header,Confidence:0,Method:"unmapped"}
        for target,aliases:=range r.aliases {
            for _,alias:=range aliases {
                score:=similarity(n,alias)
                method:="token_similarity"
                if n==alias { score=1; method="alias_exact" }
                if score>best.Confidence {
                    best.Target=target; best.Confidence=score; best.Method=method
                }
            }
        }
        if best.Confidence<0.55 {
            best.Target=""; best.Method="unmapped"
        }
        out=append(out,best)
    }
    return out
}

func InferType(samples []string) string {
    nonEmpty:=make([]string,0,len(samples))
    for _,v:=range samples { if strings.TrimSpace(v)!="" { nonEmpty=append(nonEmpty,strings.TrimSpace(v)) } }
    if len(nonEmpty)==0 { return "unknown" }

    if all(nonEmpty,func(v string)bool{
        cleaned:=strings.NewReplacer(",","","₱","","$","","€","").Replace(v)
        _,err:=strconv.ParseFloat(strings.TrimSpace(cleaned),64); return err==nil
    }) { return "decimal" }

    layouts:=[]string{"2006-01-02","02/01/2006","01/02/2006","02-Jan-2006","Jan 2, 2006"}
    if all(nonEmpty,func(v string)bool{
        for _,layout:=range layouts { if _,err:=time.Parse(layout,v); err==nil { return true } }
        return false
    }) { return "date" }

    return "string"
}

func similarity(a,b string) float64 {
    if a=="" || b=="" { return 0 }
    as,bs:=tokenSet(a),tokenSet(b)
    intersection:=0
    for token:=range as { if _,ok:=bs[token]; ok { intersection++ } }
    union:=len(as)+len(bs)-intersection
    if union==0 { return 0 }
    return float64(intersection)/float64(union)
}

func tokenSet(v string) map[string]struct{} {
    m:=map[string]struct{}{}
    for _,p:=range strings.Fields(v) { m[p]=struct{}{} }
    return m
}
func all(values []string,predicate func(string)bool) bool {
    for _,v:=range values { if !predicate(v) { return false } }
    return true
}
func dedupe(values []string) []string {
    seen:=map[string]struct{}{}; out:=make([]string,0,len(values))
    for _,v:=range values { if _,ok:=seen[v]; !ok && v!="" { seen[v]=struct{}{}; out=append(out,v) } }
    sort.Strings(out); return out
}
