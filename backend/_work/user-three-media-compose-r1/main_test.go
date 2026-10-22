package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrozenScopeLoadsExactThree(t *testing.T) {
	raw, e := os.ReadFile(scopePath)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := loadScope(raw, scopeSHA)
	if e != nil || len(scope) != 3 {
		t.Fatal("frozen scope invalid", e)
	}
	if _, e = loadScope(append(raw, ' '), scopeSHA); e == nil {
		t.Fatal("scope byte drift accepted")
	}
}
func TestRootReviewRequiredAndPinned(t *testing.T) {
	for _, valid := range []bool{false, true} {
		v := map[string]any{"head":2653,"gateway_image":"sha256:ffaa4f358647539ac36e0f210efd5eaf20534b634cd88396aa40a63cf0374821","approved": valid, "scope_sha256": scopeSHA, "physical_media_worker_evidence_sha256": strings.Repeat("a", 64), "current_quiescence_receipt_sha256": strings.Repeat("b", 64)}
		raw, _ := json.Marshal(v)
		sum := sha256.Sum256(raw)
		p := filepath.Join(t.TempDir(), "review.json")
		os.WriteFile(p, raw, 0600)
		e := reviewed(p, hex.EncodeToString(sum[:]))
		if valid != (e == nil) {
			t.Fatal("review authority mismatch")
		}
		if reviewed(p, strings.Repeat("0", 64)) == nil {
			t.Fatal("unpinned review accepted")
		}
	}
}
