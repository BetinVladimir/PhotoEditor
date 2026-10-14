package interchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestImportNativeStyleInstallationPinnedOperatorFile(t *testing.T) {
	sha := func(value string) string { return rawSHA([]byte(value)) }
	wire := importNativeStyleDeploymentWire{Schema: "interchange-import-style-installation/v1", WorkICCSHA: sha("icc"), StyleRuntimeSHA: sha("style"), ImageRuntimeSHA: sha("image"), Models: map[string]string{"portrait.hair": sha("hair")}}
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "style.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := LoadImportNativeStyleInstallation(context.Background(), path, rawSHA(raw))
	if err != nil || installed.WorkICCSHA != wire.WorkICCSHA || installed.Models["portrait.hair"] != wire.Models["portrait.hair"] {
		t.Fatal("pinned style installation unavailable", err)
	}
	if _, err := LoadImportNativeStyleInstallation(context.Background(), path, sha("wrong")); err == nil {
		t.Fatal("changed pin accepted")
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportNativeStyleInstallation(context.Background(), link, rawSHA(raw)); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportNativeStyleInstallation(context.Background(), path, rawSHA(raw)); err == nil {
		t.Fatal("shared writable operator file accepted")
	}
}

func TestImportNativeStyleInstallationRejectsUnpinnedFields(t *testing.T) {
	sha := func(value string) string { return rawSHA([]byte(value)) }
	base := importNativeStyleDeploymentWire{Schema: "interchange-import-style-installation/v1", WorkICCSHA: sha("icc"), StyleRuntimeSHA: sha("style"), ImageRuntimeSHA: sha("image"), Models: map[string]string{}}
	for _, mutate := range []func(*importNativeStyleDeploymentWire){
		func(v *importNativeStyleDeploymentWire) { v.WorkICCSHA = "" },
		func(v *importNativeStyleDeploymentWire) { v.Models = map[string]string{"portrait.hair": ""} },
		func(v *importNativeStyleDeploymentWire) { v.Models = nil },
		func(v *importNativeStyleDeploymentWire) { v.Schema = "unknown" },
	} {
		wire := base
		mutate(&wire)
		raw, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeImportNativeStyleInstallation(raw, rawSHA(raw)); err == nil {
			t.Fatal("incomplete style installation accepted")
		}
	}
}
