package interchange

import (
	"context"
	"os"
	"testing"
)

// The probe stub checks the Go preflight boundary and scratch cleanup; the
// real worker Override schema/model policy is covered by its own unit cases.
func TestNativeImportOverrideArchivePreflight(t *testing.T) {
	root := t.TempDir()
	scratch := &ExportArchiveScratch{root: root, maximum: 1 << 20}
	runtime := rawSHA([]byte("runtime"))
	probe := &exportStyleProbeStub{runtime: runtime}
	qualifier, err := NewNativeImportOverrideArchiveQualifier(scratch, probe, map[string]string{}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := qualifier.ValidateArchivedImportOverride(ctx, []byte(`{}`)); err != context.Canceled || probe.calls != 0 {
		t.Fatal("cancelled archive preflight reached native probe", err)
	}
	if err := qualifier.ValidateArchivedImportOverride(context.Background(), []byte(`{ }`)); err == nil || probe.calls != 0 {
		t.Fatal("noncanonical archive reached native probe", err)
	}
	if err := qualifier.ValidateArchivedImportOverride(context.Background(), []byte(`{}`)); err != nil || probe.calls != 1 {
		t.Fatal("archive native preflight unavailable", err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 || scratch.used != 0 {
		t.Fatal("archive preflight leaked scratch", err)
	}
}
