package interchange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadExportPolicyFileRejectsUnsafeFilesystem(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "policy.json")
	if err := os.WriteFile(valid, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readExportPolicyFile(context.Background(), valid, 1024); err != nil || string(raw) != "{}" {
		t.Fatal("regular operator file rejected", err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(valid, link); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(root, "hard.json")
	if err := os.Link(valid, hard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"policy.json", link, hard, root} {
		if raw, err := readExportPolicyFile(context.Background(), path, 1024); err == nil || raw != nil {
			t.Fatalf("unsafe policy file accepted: %s", path)
		}
	}
	if err := os.Remove(hard); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(valid, 0666); err != nil {
		t.Fatal(err)
	}
	if raw, err := readExportPolicyFile(context.Background(), valid, 1024); err == nil || raw != nil {
		t.Fatal("writable operator file accepted")
	}
	if err := os.Chmod(valid, 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readExportPolicyFile(context.Background(), valid, 1); err == nil || raw != nil {
		t.Fatal("invalid limit accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err := readExportPolicyFile(ctx, valid, 1024); !errors.Is(err, context.Canceled) || raw != nil {
		t.Fatal("cancellation lost", err)
	}
}
