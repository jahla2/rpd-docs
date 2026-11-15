package config

import "os"

type Config struct {
    DatabaseURL string
    NATSURL string
    StorageRoot string
    NativeEnginePath string
    AIEngineURL string
}

func Load() Config {
    return Config{
        DatabaseURL: value("DATABASE_URL","postgres://rpd:rpd@localhost:5432/rpd?sslmode=disable"),
        NATSURL: value("NATS_URL","nats://localhost:4222"),
        StorageRoot: value("STORAGE_ROOT","./storage"),
        NativeEnginePath: value("NATIVE_ENGINE_PATH","./build/native-engine/rpd-native-engine"),
        AIEngineURL: value("AI_ENGINE_URL","http://localhost:8000"),
    }
}
func value(key,fallback string) string { if v:=os.Getenv(key); v!="" { return v }; return fallback }
