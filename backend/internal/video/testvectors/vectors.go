// Package testvectors loads shared video test vectors from docs/video/vectors.
package testvectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

var nameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// Load reads (VIDEO_VECTORS_DIR overrides the directory) docs/video/vectors/<name>.json into out.
func Load(t *testing.T, name string, out any) {
	t.Helper()
	if !nameRe.MatchString(name) {
		t.Fatalf("bad vector name %q", name)
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "docs", "video", "vectors")
	if d := os.Getenv("VIDEO_VECTORS_DIR"); d != "" { // a test binary built elsewhere and run on another host
		dir = d
	}
	path := filepath.Join(dir, name+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vector %s: %v", name, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode vector %s: %v", name, err)
	}
}
