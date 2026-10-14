package interchange

import (
	"context"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"strings"
	"syscall"
	"testing"
)

func TestLoadNativeExportDeploymentExactPinnedRegularFile(t *testing.T) {
	raw, err := canonical.Marshal(nativeExportDeploymentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "export.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadNativeExportDeployment(context.Background(), path, rawSHA(raw))
	if err != nil || d.raw != string(raw) {
		t.Fatal(err)
	}
	if _, err = LoadNativeExportDeployment(context.Background(), path, strings.Repeat("b", 64)); err == nil {
		t.Fatal("foreign manifest digest accepted")
	}
	link := filepath.Join(root, "linked.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeExportDeployment(context.Background(), link, rawSHA(raw)); err == nil {
		t.Fatal("symlink manifest accepted")
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeExportDeployment(context.Background(), path, rawSHA(raw)); err == nil {
		t.Fatal("shared writable manifest accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = LoadNativeExportDeployment(ctx, path, rawSHA(raw)); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err = LoadNativeExportDeployment(context.Background(), "relative.json", rawSHA(raw)); err == nil {
		t.Fatal("relative deployment path accepted")
	}
}

func TestNativeExportDeploymentRejectsUnsafeFileEnvelope(t *testing.T) {
	for _, mode := range []string{"hardlink", "fifo", "directory", "empty", "oversized", "unnormalized", "world_writable"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := canonical.Marshal(nativeExportDeploymentFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path := filepath.Join(root, "export.json")
			switch mode {
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "empty":
				err = os.WriteFile(path, nil, 0600)
			case "oversized":
				var file *os.File
				file, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
				if err == nil {
					err = file.Truncate((9 << 20) + 1)
					file.Close()
				}
			default:
				err = os.WriteFile(path, raw, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "hardlink":
				if err = os.Link(path, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
			case "unnormalized":
				path = root + "/./export.json"
			case "world_writable":
				if err = os.Chmod(path, 0622); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = LoadNativeExportDeployment(context.Background(), path, rawSHA(raw)); err == nil {
				t.Fatal("unsafe deployment file accepted")
			}
		})
	}
}
