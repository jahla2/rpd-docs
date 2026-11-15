package reconciliation

import (
    "fmt"
    "math/big"
    "sort"
)

type AmountEntry struct {
    ID string
    Amount string
}

type GroupCombination struct {
    IDs []string
    Total string
    Difference string
}

type parsedAmountEntry struct {
    ID string
    Amount *big.Rat
}

func FindAmountCombination(entries []AmountEntry,targetValue,toleranceValue string,maxSize int)(GroupCombination,bool,error){
    target,ok:=new(big.Rat).SetString(targetValue)
    if !ok { return GroupCombination{},false,fmt.Errorf("invalid target amount %q",targetValue) }
    tolerance,ok:=new(big.Rat).SetString(toleranceValue)
    if !ok || tolerance.Sign()<0 { return GroupCombination{},false,fmt.Errorf("invalid tolerance %q",toleranceValue) }
    if maxSize<2 { return GroupCombination{},false,nil }
    if maxSize>6 { maxSize=6 }

    parsed:=make([]parsedAmountEntry,0,len(entries))
    for _,entry:=range entries {
        amount,ok:=new(big.Rat).SetString(entry.Amount)
        if !ok || amount.Sign()<=0 { continue }
        parsed=append(parsed,parsedAmountEntry{ID:entry.ID,Amount:amount})
    }
    // Bound combinatorial work. Candidate generation should already rank the
    // most relevant ERP rows; searching the first 30 keeps worst-case work sane.
    if len(parsed)>30 { parsed=parsed[:30] }

    sort.SliceStable(parsed,func(i,j int)bool{
        return parsed[i].Amount.Cmp(parsed[j].Amount)>0
    })

    upper:=new(big.Rat).Add(new(big.Rat).Set(target),tolerance)
    bestDiff:=new(big.Rat).SetInt64(1<<62)
    best:=GroupCombination{}

    var search func(start int,chosen []parsedAmountEntry,sum *big.Rat)
    search=func(start int,chosen []parsedAmountEntry,sum *big.Rat){
        if len(chosen)>=2 {
            diff:=absRatDiff(sum,target)
            if diff.Cmp(tolerance)<=0 && diff.Cmp(bestDiff)<0 {
                ids:=make([]string,len(chosen))
                for i,item:=range chosen { ids[i]=item.ID }
                best=GroupCombination{IDs:ids,Total:sum.FloatString(4),Difference:diff.FloatString(4)}
                bestDiff=new(big.Rat).Set(diff)
                if diff.Sign()==0 { return }
            }
        }
        if len(chosen)>=maxSize { return }

        for i:=start;i<len(parsed);i++ {
            next:=new(big.Rat).Add(sum,parsed[i].Amount)
            if next.Cmp(upper)>0 { continue }
            nextChosen:=append(append([]parsedAmountEntry(nil),chosen...),parsed[i])
            search(i+1,nextChosen,next)
            if bestDiff.Sign()==0 { return }
        }
    }

    search(0,nil,new(big.Rat))
    return best,len(best.IDs)>0,nil
}

func absRatDiff(a,b *big.Rat)*big.Rat{
    diff:=new(big.Rat).Sub(a,b)
    if diff.Sign()<0 { diff.Neg(diff) }
    return diff
}
