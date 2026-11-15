package auth

import (
    "context"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base64"
    "encoding/hex"
    "fmt"
    "strings"
)

type Principal struct {
    APIKeyID string
    TenantID string
    ActorID string
    Role string
    Permissions map[string]bool
}

type contextKey struct{}

func WithPrincipal(ctx context.Context,p Principal) context.Context {
    return context.WithValue(ctx,contextKey{},p)
}

func PrincipalFrom(ctx context.Context)(Principal,bool){
    p,ok:=ctx.Value(contextKey{}).(Principal)
    return p,ok
}

func GenerateAPIKey()(token,prefix string,err error){
    prefixBytes:=make([]byte,6)
    secretBytes:=make([]byte,32)
    if _,err=rand.Read(prefixBytes); err!=nil { return "","",err }
    if _,err=rand.Read(secretBytes); err!=nil { return "","",err }
    prefix=hex.EncodeToString(prefixBytes)
    secret:=base64.RawURLEncoding.EncodeToString(secretBytes)
    token="rpd_"+prefix+"_"+secret
    return token,prefix,nil
}

func ParseAPIKey(token string)(string,error){
    token=strings.TrimSpace(token)
    parts:=strings.Split(token,"_")
    if len(parts)!=3 || parts[0]!="rpd" || len(parts[1])<8 || len(parts[2])<20 {
        return "",fmt.Errorf("invalid API key format")
    }
    return parts[1],nil
}

func HashToken(token,pepper string) string {
    mac:=hmac.New(sha256.New,[]byte(pepper))
    _,_=mac.Write([]byte(token))
    return hex.EncodeToString(mac.Sum(nil))
}

func VerifyToken(token,pepper,expectedHex string) bool {
    actual:=HashToken(token,pepper)
    if len(actual)!=len(expectedHex) { return false }
    return subtle.ConstantTimeCompare([]byte(actual),[]byte(expectedHex))==1
}
