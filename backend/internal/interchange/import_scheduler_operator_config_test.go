package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func importSchedulerConfigFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	var wire importSchedulerOperatorConfig
	wire.Schema = "interchange-import-scheduler-operator/v1"
	sha := rawSHA([]byte("operator pin"))
	wire.Native.Path, wire.Native.SHA = "/operator/native.json", sha
	wire.Style.Path, wire.Style.SHA = "/operator/style.json", sha
	wire.Denoise.Path, wire.Denoise.SHA = "/operator/denoise.json", sha
	wire.Upscale.Path, wire.Upscale.SHA = "/operator/upscale.json", sha
	wire.Stats.Path, wire.Stats.SHA = "/operator/stats.json", sha
	wire.Person.Path, wire.Person.SHA = "/operator/person.json", sha
	wire.ArchiveBytes, wire.ExpandedBytes, wire.Files = 1<<30, 2<<30, 30000
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return raw, rawSHA(raw)
}

func TestImportSchedulerOperatorConfigPinnedStrictLoad(t *testing.T) {
	raw, pin := importSchedulerConfigFixture(t)
	files, err := DecodeImportSchedulerOperatorFiles(raw, pin)
	if err != nil || files.Recipes.Person.Path != "/operator/person.json" || files.Limits.Files != 30000 {
		t.Fatal("complete operator declaration was not decoded", err)
	}
	path := filepath.Join(t.TempDir(), "operator.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportSchedulerOperatorFiles(context.Background(), path, pin); err != nil {
		t.Fatal("pinned operator declaration was not loaded", err)
	}
	var widened map[string]any
	if err := json.Unmarshal(raw, &widened); err != nil {
		t.Fatal(err)
	}
	widened["unexpected"] = true
	unknown, err := canonical.Marshal(widened)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte(nil), raw...), ' '),
		[]byte(`{"schema_version":"unknown"}`),
		unknown,
	} {
		if _, err := DecodeImportSchedulerOperatorFiles(invalid, rawSHA(invalid)); err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	if _, err := DecodeImportSchedulerOperatorFiles(raw, rawSHA([]byte("foreign"))); err == nil {
		t.Fatal("foreign declaration pin accepted")
	}
	if _, err := LoadImportSchedulerOperatorFiles(context.Background(), "relative.json", pin); err == nil {
		t.Fatal("relative operator declaration path accepted")
	}
	link := filepath.Join(t.TempDir(), "operator-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportSchedulerOperatorFiles(context.Background(), link, pin); err == nil {
		t.Fatal("symlink operator declaration accepted")
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImportSchedulerOperatorFiles(context.Background(), path, pin); err == nil {
		t.Fatal("group/world writable operator declaration accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadImportSchedulerOperatorFiles(ctx, path, pin); !errors.Is(err, context.Canceled) {
		t.Fatal("operator loader lost cancellation", err)
	}
}
