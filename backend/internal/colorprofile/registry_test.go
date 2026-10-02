package colorprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func copyRegistry(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := "../../../resources/icc"
	for _, name := range []string{"profiles.lock.json", "work_rec2020_gamma24_v1.icc", "srgb_v1.icc", "linear_srgb_v1.icc"} {
		raw, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func TestLoadBundledWorkICC(t *testing.T) {
	sha, err := LoadWorkSHA("../../../resources/icc")
	if err != nil || sha != "a525b6d4afd90340bc8f69535dae83594925224f3569a3b90b988ac5f937e6e9" {
		t.Fatal("bundled profile rejected", err)
	}
}
func TestLoadBundledICCMap(t *testing.T) {
	profiles, err := LoadSHAs("../../../resources/icc")
	if err != nil || len(profiles) != 3 || profiles["srgb_v1"] == "" || profiles["work_rec2020_gamma24_v1"] == "" {
		t.Fatal("bundled profile map rejected", profiles, err)
	}
}
func TestRegistryRejectsCorruptMissingAndEscapingProfiles(t *testing.T) {
	for _, mode := range []string{"digest", "header", "path", "duplicate", "missing", "symlink", "unknown field"} {
		t.Run(mode, func(t *testing.T) {
			root := copyRegistry(t)
			path := filepath.Join(root, "profiles.lock.json")
			raw, _ := os.ReadFile(path)
			var lock manifest
			if err := json.Unmarshal(raw, &lock); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "digest":
				lock.Profiles[0].SHA256 = lock.Profiles[1].SHA256
			case "path":
				lock.Profiles[0].Path = "../escape.icc"
			case "duplicate":
				lock.Profiles[0] = lock.Profiles[1]
			case "missing":
				lock.Profiles = lock.Profiles[:2]
			case "header":
				p := filepath.Join(root, lock.Profiles[0].Path)
				data, _ := os.ReadFile(p)
				data[36] = 'x'
				sum := sha256.Sum256(data)
				lock.Profiles[0].SHA256 = hex.EncodeToString(sum[:])
				os.WriteFile(p, data, 0600)
			case "symlink":
				p := filepath.Join(root, lock.Profiles[0].Path)
				os.Remove(p)
				if err := os.Symlink(lock.Profiles[1].Path, p); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ = json.Marshal(lock)
			if mode == "unknown field" {
				raw = append(raw[:len(raw)-1], []byte(`,"extra":1}`)...)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadWorkSHA(root); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}
