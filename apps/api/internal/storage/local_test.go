package storage

import "testing"

func TestSafeExtension(t *testing.T) {
    tests := map[string]string{
        "invoice.PDF": ".pdf",
        "../../bad.exe": ".bin",
        "sheet.xlsx": ".xlsx",
        "photo.jpeg": ".jpeg",
    }
    for input, want := range tests {
        if got := safeExtension(input); got != want {
            t.Fatalf("safeExtension(%q)=%q want %q", input, got, want)
        }
    }
}
