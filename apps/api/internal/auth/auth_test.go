package auth

import "testing"

func TestGenerateParseAndVerifyAPIKey(t *testing.T){
    token,prefix,err:=GenerateAPIKey()
    if err!=nil { t.Fatal(err) }
    parsed,err:=ParseAPIKey(token)
    if err!=nil { t.Fatal(err) }
    if parsed!=prefix { t.Fatalf("prefix=%q parsed=%q",prefix,parsed) }
    hash:=HashToken(token,"pepper")
    if !VerifyToken(token,"pepper",hash) { t.Fatal("verification failed") }
    if VerifyToken(token+"x","pepper",hash) { t.Fatal("modified token verified") }
}
