package colorprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockSHARejectsIncompleteRegistry(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadLockSHA(root); err == nil {
		t.Fatal("missing registry accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "profiles.lock.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLockSHA(root); err == nil {
		t.Fatal("unvalidated lock accepted")
	}
}
