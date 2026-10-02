package canonical

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"testing"
)

//go:embed vectors.json
var vectorsFS embed.FS

func TestPythonCanonicalAgreement(t *testing.T) {
	raw, err := vectorsFS.ReadFile("vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct{ Input, Canonical, SHA256 string }
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		got, err := Bytes([]byte(v.Input))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(got)
		if string(got) != v.Canonical || hex.EncodeToString(hash[:]) != v.SHA256 {
			t.Fatalf("Go/Python JCS mismatch: %s", got)
		}
	}
}
