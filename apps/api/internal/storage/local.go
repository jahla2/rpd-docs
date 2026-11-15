package storage

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"
)

type Local struct { Root string }
type SavedOriginal struct { Path string; SHA256 string; Size int64 }

func NewLocal(root string) *Local { return &Local{Root: root} }

func (s *Local) SaveOriginal(tenantID, documentID, originalName string, src io.Reader) (SavedOriginal, error) {
    now := time.Now().UTC()
    dir := filepath.Join(s.Root, "tenants", tenantID, "inbound",
        fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", int(now.Month())), documentID)
    if err := os.MkdirAll(dir, 0o750); err != nil { return SavedOriginal{}, err }

    target := filepath.Join(dir, "original"+safeExtension(originalName))
    f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
    if err != nil { return SavedOriginal{}, err }
    defer f.Close()

    h := sha256.New()
    n, err := io.Copy(io.MultiWriter(f, h), src)
    if err != nil { _ = os.Remove(target); return SavedOriginal{}, err }
    digest := hex.EncodeToString(h.Sum(nil))

    manifest := map[string]any{
        "schema_version": "1.0", "document_id": documentID,
        "original_filename": filepath.Base(originalName), "sha256": digest,
        "size_bytes": n, "stored_at": now.Format(time.RFC3339Nano),
    }
    manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
    if err := os.WriteFile(filepath.Join(dir, "upload_manifest.json"), manifestBytes, 0o640); err != nil {
        return SavedOriginal{}, err
    }
    abs, err := filepath.Abs(target)
    if err != nil { return SavedOriginal{}, err }
    return SavedOriginal{Path: abs, SHA256: digest, Size: n}, nil
}

func (s *Local) Ready() error {
    if err := os.MkdirAll(s.Root, 0o750); err != nil { return err }
    probe := filepath.Join(s.Root, ".ready")
    if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil { return err }
    return os.Remove(probe)
}

func safeExtension(name string) string {
    ext := strings.ToLower(filepath.Ext(filepath.Base(name)))
    switch ext {
    case ".pdf", ".png", ".jpg", ".jpeg", ".tif", ".tiff", ".csv", ".xlsx":
        return ext
    default:
        return ".bin"
    }
}
