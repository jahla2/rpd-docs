package main

import (
    "context"
    "flag"
    "fmt"
    "os"
    "strings"

    "github.com/jahla2/rpd-docs/apps/api/internal/auth"
    "github.com/jahla2/rpd-docs/apps/api/internal/config"
    "github.com/jahla2/rpd-docs/apps/api/internal/store"
)

func main(){
    name:=flag.String("name","bootstrap-admin","key name")
    tenant:=flag.String("tenant","00000000-0000-0000-0000-000000000001","tenant UUID")
    role:=flag.String("role","ADMIN","ADMIN|OPERATOR|REVIEWER|READER")
    flag.Parse()

    cfg:=config.Load()
    if cfg.APIKeyPepper=="" { fmt.Fprintln(os.Stderr,"API_KEY_PEPPER is required"); os.Exit(1) }
    st,err:=store.New(context.Background(),cfg.DatabaseURL)
    if err!=nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) }
    defer st.Close()

    token,prefix,err:=auth.GenerateAPIKey()
    if err!=nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) }
    normalizedRole:=strings.ToUpper(*role)
    info,err:=st.CreateAPIKey(context.Background(),*tenant,*name,prefix,auth.HashToken(token,cfg.APIKeyPepper),normalizedRole,"keyctl",nil)
    if err!=nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) }

    fmt.Printf("API key created: %s (%s)\n",info.Name,info.Role)
    fmt.Printf("Token (shown once): %s\n",token)
}
