package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestDefaultRenderPolicyCanonicalAndStrict(t *testing.T) {
	snapshot, err := DefaultRenderPolicy()
	if err != nil {
		t.Fatal(err)
	}
	// Independent exact wire for existing SQL child behavior and planner bound.
	expected := `{"child_failure_policy":"fail_fast","max_frames":5000,"renderer_version":"v2.2","schema_version":1,"workflow_type":"preview"}`
	sum := sha256.Sum256([]byte(expected))
	if snapshot.PolicySHA != hex.EncodeToString(sum[:]) {
		t.Fatal("policy digest does not bind fixed semantics")
	}
	raw, _ := canonical.Marshal(snapshot)
	if decoded, err := DecodeRenderPolicySnapshot(raw); err != nil || decoded != snapshot {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"max_frames":5000`, `"max_frames":5001`, 1), strings.Replace(string(raw), `"policy":`, `"Policy":`, 1), strings.Replace(string(raw), `"policy":`, `"runtime":"cuda","policy":`, 1), strings.Replace(string(raw), `"schema_version":1`, `"schema_version":null`, 1), strings.Replace(string(raw), snapshot.PolicySHA, strings.Repeat("a", 64), 1)} {
		if _, err := DecodeRenderPolicySnapshot([]byte(bad)); err == nil {
			t.Fatal("unsupported/unbound policy accepted")
		}
	}
}
