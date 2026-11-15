package config

import "os"

type Config struct {
    HTTPAddr string
    DatabaseURL string
    NATSURL string
    StorageRoot string
    DefaultTenantID string
    AuthMode string
    APIKeyPepper string
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
    }
}

func value(key, fallback string) string {
    if v := os.Getenv(key); v != "" { return v }
    return fallback
}
