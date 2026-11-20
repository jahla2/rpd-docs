package config

import (
    "os"
    "strconv"
)

type Config struct {
    HTTPAddr string
    DatabaseURL string
    NATSURL string
    StorageRoot string
    DefaultTenantID string
    AuthMode string
    APIKeyPepper string
    StorageBackend string
    S3Endpoint string
    S3Region string
    S3Bucket string
    S3AccessKey string
    S3SecretKey string
    S3CreateBucket bool
    RateLimitRPS float64
    RateLimitBurst int
    AIEngineURL string
    NativeEnginePath string
}

func Load() Config {
    return Config{
        HTTPAddr: value("HTTP_ADDR", ":8080"),
        DatabaseURL: value("DATABASE_URL", "postgres://rpd:rpd@localhost:5432/rpd?sslmode=disable"),
        NATSURL: value("NATS_URL", "nats://localhost:4222"),
        StorageRoot: value("STORAGE_ROOT", "./storage"),
        DefaultTenantID: value("DEFAULT_TENANT_ID", "00000000-0000-0000-0000-000000000001"),
        AuthMode: value("AUTH_MODE", "disabled"),
        APIKeyPepper: value("API_KEY_PEPPER", ""),
        StorageBackend: value("STORAGE_BACKEND", "local"),
        S3Endpoint: value("S3_ENDPOINT", ""),
        S3Region: value("S3_REGION", "us-east-1"),
        S3Bucket: value("S3_BUCKET", ""),
        S3AccessKey: value("S3_ACCESS_KEY", ""),
        S3SecretKey: value("S3_SECRET_KEY", ""),
        S3CreateBucket: boolValue("S3_CREATE_BUCKET", false),
        RateLimitRPS: floatValue("RATE_LIMIT_RPS",20),
        RateLimitBurst: intValue("RATE_LIMIT_BURST",40),
        AIEngineURL: value("AI_ENGINE_URL","http://localhost:8000"),
        NativeEnginePath: value("NATIVE_ENGINE_PATH","./build/native-engine/rpd-native-engine"),
    }
}

func value(key, fallback string) string {
    if v := os.Getenv(key); v != "" { return v }
    return fallback
}

func boolValue(key string,fallback bool) bool { v:=os.Getenv(key); if v=="" { return fallback }; return v=="1" || v=="true" || v=="TRUE" || v=="yes" || v=="YES" }

func floatValue(key string,fallback float64) float64 { if v:=os.Getenv(key); v!="" { if parsed,err:=strconv.ParseFloat(v,64); err==nil && parsed>0 { return parsed } }; return fallback }
func intValue(key string,fallback int) int { if v:=os.Getenv(key); v!="" { if parsed,err:=strconv.Atoi(v); err==nil && parsed>0 { return parsed } }; return fallback }
