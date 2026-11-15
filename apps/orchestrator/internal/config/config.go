package config

import "os"

type Config struct {
    DatabaseURL string
    NATSURL string
    StorageRoot string
    NativeEnginePath string
    AIEngineURL string
    AgentFallbackEnabled bool
    AgentMinConfidence string
}

func Load() Config {
    return Config{
        DatabaseURL: value("DATABASE_URL","postgres://rpd:rpd@localhost:5432/rpd?sslmode=disable"),
        NATSURL: value("NATS_URL","nats://localhost:4222"),
        StorageRoot: value("STORAGE_ROOT","./storage"),
        NativeEnginePath: value("NATIVE_ENGINE_PATH","./build/native-engine/rpd-native-engine"),
        AIEngineURL: value("AI_ENGINE_URL","http://localhost:8000"),
        AgentFallbackEnabled: boolValue("AGENT_FALLBACK_ENABLED",false),
        AgentMinConfidence: value("AGENT_MIN_CONFIDENCE","0.90"),
    }
}
func value(key,fallback string) string { if v:=os.Getenv(key); v!="" { return v }; return fallback }

func boolValue(key string,fallback bool) bool {
    v:=os.Getenv(key)
    if v=="" { return fallback }
    return v=="1" || v=="true" || v=="TRUE" || v=="yes" || v=="YES"
}
