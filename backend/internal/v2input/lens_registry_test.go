package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestLensRegistryPinsExactOperatorFile(t *testing.T) {
	// Synthetic runtime exercises registry IO only, not installed-runtime qualification.
	components := map[string]string{}
	for _, name := range []string{"python", "numpy_core", "pydantic_core", "canonical_impl", "lens_algorithm", "lens_prepare", "lens_color", "lens_adapter", "lens_result", "lens_resources", "lens_admission", "lens_metadata_source", "lens_exif"} {
		components[name] = strings.Repeat("d", 64)
	}
	runtime := map[string]any{"schema_version": 1, "python_version": "3.12.1", "implementation": "CPython", "machine": "synthetic", "packages": map[string]string{"numpy": "test", "pydantic": "test", "rfc8785": "test"}, "components": components}
	runtimeSHA, _ := canonical.Hash(runtime)
	qualification := Ref{uuid.New(), strings.Repeat("a", 64)}
	profile := map[string]any{"qualification": qualification, "coefficients_sha": strings.Repeat("b", 64), "runtime_lock_sha": runtimeSHA, "dataset_sha": strings.Repeat("c", 64)}
	registry := map[string]any{"schema_version": 1, "runtime": runtime, "profiles": []any{profile}}
	path := filepath.Join(t.TempDir(), "registry.json")
	write := func(value any) string {
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		return hex.EncodeToString(hash[:])
	}
	digest := write(registry)
	result, err := LoadLensRegistry(path, digest)
	if err != nil || result.RuntimeLockSHA != runtimeSHA || len(result.Pins) != 1 || result.Pins[0].Qualification != qualification {
		t.Fatal("operator registry", result, err)
	}
	if _, err = LoadLensRegistry(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("raw digest substitution accepted")
	}
	link := path + ".symlink"
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadLensRegistry(link, digest); err == nil {
		t.Fatal("symlink accepted")
	}
	hard := path + ".hardlink"
	if err = os.Link(path, hard); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadLensRegistry(path, digest); err == nil {
		t.Fatal("multiply linked registry accepted")
	}
	if err = os.Remove(hard); err != nil {
		t.Fatal(err)
	}
	registry["profiles"] = []any{profile, profile}
	digest = write(registry)
	if _, err = LoadLensRegistry(path, digest); err == nil {
		t.Fatal("duplicate operator pin accepted")
	}
	registry["profiles"] = []any{profile}
	profile["runtime_lock_sha"] = strings.Repeat("0", 64)
	digest = write(registry)
	if _, err = LoadLensRegistry(path, digest); err == nil {
		t.Fatal("incoherent runtime accepted")
	}
	profile["runtime_lock_sha"] = runtimeSHA
	runtime["implementation"] = "Other"
	digest = write(registry)
	if _, err = LoadLensRegistry(path, digest); err == nil {
		t.Fatal("worker-incompatible runtime accepted")
	}
	runtime["implementation"] = "CPython"
	registry["unexpected"] = true
	digest = write(registry)
	if _, err = LoadLensRegistry(path, digest); err == nil {
		t.Fatal("unknown registry field accepted")
	}
}
