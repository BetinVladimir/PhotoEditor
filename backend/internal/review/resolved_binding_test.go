package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestResolvedBindingFullBodyHash(t *testing.T) {
	raw, err := os.ReadFile("testdata/resolved-binding-python.json")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ParseResolvedBinding(raw)
	if err != nil || binding.Mode != "skip" {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"modules", "strength", "local", "params"} {
		t.Run(key, func(t *testing.T) {
			var d map[string]any
			json.Unmarshal(raw, &d)
			d[key] = nil
			b, _ := canonical.Marshal(d)
			if _, err := ParseResolvedBinding(b); err == nil {
				t.Fatal("changed body accepted")
			}
		})
	}
	cases := map[string]func(map[string]any){
		"unknown":          func(d map[string]any) { d["extra"] = true },
		"missing":          func(d map[string]any) { delete(d, "distance") },
		"null boolean":     func(d map[string]any) { d["params"].(map[string]any)["strict_ood"] = nil },
		"missing optional": func(d map[string]any) { delete(d["params"].(map[string]any), "stats") },
		"invalid image":    func(d map[string]any) { d["params"].(map[string]any)["image"].(map[string]any)["sha256"] = "bad" },
		"unknown mode":     func(d map[string]any) { d["mode"] = "other" },
		"skip nonstrict":   func(d map[string]any) { d["params"].(map[string]any)["strict_ood"] = false },
		"group mismatch":   func(d map[string]any) { d["mode"] = "group_fixed" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var d map[string]any
			json.Unmarshal(raw, &d)
			change(d)
			delete(d, "hash")
			b, _ := canonical.Marshal(d)
			h := sha256.Sum256(b)
			d["hash"] = hex.EncodeToString(h[:])
			b, _ = canonical.Marshal(d)
			if _, err := ParseResolvedBinding(b); err == nil {
				t.Fatal("invalid binding accepted with recomputed hash")
			}
		})
	}
	if _, err := ParseResolvedBinding(append(raw[:len(raw)-1], []byte(`,"hash":"bad"}`)...)); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
