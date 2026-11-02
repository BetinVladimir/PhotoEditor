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

func TestLockSHAReturnsDeterministicHash(t *testing.T) {
	root := copyRegistry(t)
	sha, err := LoadLockSHA(root)
	if err != nil {
		t.Fatalf("valid registry rejected: %v", err)
	}
	if len(sha) != 64 {
		t.Fatalf("SHA256 hex must be 64 chars, got %d", len(sha))
	}
	sha2, err := LoadLockSHA(root)
	if err != nil {
		t.Fatal(err)
	}
	if sha != sha2 {
		t.Fatal("LoadLockSHA must be deterministic for unchanged registry")
	}
}

