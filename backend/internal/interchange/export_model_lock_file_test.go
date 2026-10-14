package interchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportModelLockRequiresPinnedCanonicalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.lock.json")
	raw := []byte(`{"model":"renderer","schema_version":1}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if pin, err := LoadExportModelLockSHA(context.Background(), path, rawSHA(raw)); err != nil || pin != rawSHA(raw) {
		t.Fatal("pinned model lock unavailable", err)
	}
	if pin, err := LoadExportModelLockSHA(context.Background(), path, rawSHA([]byte("foreign"))); err == nil || pin != "" {
		t.Fatal("foreign model lock pin accepted")
	}
	link := filepath.Join(filepath.Dir(path), "linked.lock.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if pin, err := LoadExportModelLockSHA(context.Background(), link, rawSHA(raw)); err == nil || pin != "" {
		t.Fatal("linked model lock accepted")
	}
	uncanonical := []byte(`{ "schema_version": 1 }`)
	if err := os.WriteFile(path, uncanonical, 0600); err != nil {
		t.Fatal(err)
	}
	if pin, err := LoadExportModelLockSHA(context.Background(), path, rawSHA(uncanonical)); err == nil || pin != "" {
		t.Fatal("noncanonical model lock accepted")
	}
}
