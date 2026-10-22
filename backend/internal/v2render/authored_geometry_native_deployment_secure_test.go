package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAuthoredNativeDeploymentFileSubstitutionDoesNotBlock(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink", "new-regular-inode"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "operator.json")
			raw, pin := AuthoredNativeDeploymentSecureFixture(t)
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			// Actual complete loader positive with independently computed correct
			// pin precedes substitution; the boundary helper has no SHA shortcut.
			if _, e := ReadAuthoredNativeDeployment(context.Background(), path, pin); e != nil {
				t.Fatal("actual pinned loader baseline", e)
			}
			alternative := path + ".replacement"
			if kind == "new-regular-inode" {
				if e := os.WriteFile(alternative, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			before, e := os.Lstat(path)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.Remove(path); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "fifo":
				if e = syscall.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				target := path + ".target"
				if e = os.WriteFile(target, []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			default:
				if e = os.Rename(alternative, path); e != nil {
					t.Fatal(e)
				}
			}
			done := make(chan error, 1)
			go func() {
				file, e := openAuthoredNativeDeploymentFile(path, before)
				if file != nil {
					file.Close()
				}
				done <- e
			}()
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("replaced policy file accepted")
				}
			case <-time.After(time.Second):
				// Regression rescue: unblock the old O_RDONLY FIFO open so the test does
				// not strand a goroutine. This descriptor is never used by the loader.
				rescue, e := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
				if e == nil {
					rescue.Close()
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("file opener failed to drain")
				}
				t.Fatal("private deployment open blocked after FIFO substitution")
			}
		})
	}
}
func TestAuthoredNativeDeploymentFileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator.json")
	if e := os.WriteFile(path, []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	before, e := os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	file, e := openAuthoredNativeDeploymentFile(path, before)
	if e != nil {
		t.Fatal("real regular private baseline", e)
	}
	file.Close()
	if e = os.Chmod(path, 0640); e != nil {
		t.Fatal(e)
	}
	if file, e = openAuthoredNativeDeploymentFile(path, before); e == nil {
		file.Close()
		t.Fatal("privacy drift accepted")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	before, e = os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(path, time.Unix(1, 0), before.ModTime()); e != nil {
		t.Fatal(e)
	}
	if file, e = openAuthoredNativeDeploymentFile(path, before); e != nil {
		t.Fatal("access-time-only drift refused", e)
	}
	file.Close()
	if e = os.Chtimes(path, time.Now(), before.ModTime().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if file, e = openAuthoredNativeDeploymentFile(path, before); e == nil {
		file.Close()
		t.Fatal("mtime drift accepted")
	}
}

func AuthoredNativeDeploymentSecureFixture(t *testing.T) ([]byte, string) {
	t.Helper() // Selector-only fixture: no report is claimed verified or SQL qualified.
	expected := v2qualification.Expected{Operation: "image.upscale", Version: "1", Backend: "cpu", LicenseCleared: true}
	raw, e := canonical.Marshal(map[string]any{"schema_version": 1, "contracts_sha": AuthoredGeometrySchemasSHA, "upscale": map[string]any{"expected": expected, "report": v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}})
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}
