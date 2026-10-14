package interchange

import (
	"context"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestLoadNativeImportDeploymentExactPinnedRegularFile(t *testing.T) {
	raw, err := canonical.Marshal(nativeImportDeploymentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "import.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadNativeImportDeployment(context.Background(), path, rawSHA(raw))
	if err != nil || d.raw != string(raw) {
		t.Fatal(err)
	}
	if _, err = LoadNativeImportDeployment(context.Background(), path, strings.Repeat("b", 64)); err == nil {
		t.Fatal("foreign manifest digest accepted")
	}
	link := filepath.Join(root, "linked.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeImportDeployment(context.Background(), link, rawSHA(raw)); err == nil {
		t.Fatal("symlink manifest accepted")
	}
	hardlink := filepath.Join(root, "hardlinked.json")
	if err = os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeImportDeployment(context.Background(), path, rawSHA(raw)); err == nil {
		t.Fatal("hardlinked native deployment accepted")
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeImportDeployment(context.Background(), path, rawSHA(raw)); err == nil {
		t.Fatal("shared writable manifest accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = LoadNativeImportDeployment(ctx, path, rawSHA(raw)); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err = LoadNativeImportDeployment(context.Background(), "relative.json", rawSHA(raw)); err == nil {
		t.Fatal("relative deployment path accepted")
	}
}

func TestLoadNativeImportDeploymentRejectsNoncanonicalAndOversizedFiles(t *testing.T) {
	raw, err := canonical.Marshal(nativeImportDeploymentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "import.json")
	noncanonical := append(append([]byte(nil), raw...), byte('\n'))
	if err := os.WriteFile(path, noncanonical, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativeImportDeployment(context.Background(), path, rawSHA(noncanonical)); err == nil {
		t.Fatal("noncanonical manifest accepted despite exact digest")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((9 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativeImportDeployment(context.Background(), path, rawSHA(raw)); err == nil {
		t.Fatal("oversized deployment read")
	}
}

func nativeImportDeploymentFixture(t *testing.T) nativeImportDeploymentWire {
	t.Helper()
	sha := func(s string) string { return rawSHA([]byte(s)) }
	report := ImportResourceMeasurements{Schema: "interchange-import-resource-measurements/v1", Runtime: sha("runtime"), Contracts: sha("contracts"), Hardware: sha("hardware"), Measurements: []ImportResourceMeasurement{{DocumentsSHA: sha("documents"), InputDigest: sha("inputs"), RAM: 1 << 30, Scratch: 8 << 20, Output: 32 << 20}}}
	raw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return nativeImportDeploymentWire{Schema: "interchange-import-deployment/v1", Python: "/operator/python", WorkerRoot: "/operator/workers", ScratchRoot: "/operator/scratch", ScratchBytes: 1 << 30, TimeoutSeconds: 30, ArrayRuntime: sha("array"), Runtime: report.Runtime, Contracts: report.Contracts, Hardware: report.Hardware, MeasurementSHA: rawSHA(raw), Measurements: raw, MaxInputBytes: 1 << 30}
}
