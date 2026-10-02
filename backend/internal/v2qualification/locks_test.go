package v2qualification

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLoadLocksRejectsMutableOrInconsistentPolicy(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 64) }
	locks := DeploymentLocks{Operation: "identity.compare", Version: "1",
		Backend: "cuda", ModelLockSHA: sha("a"), RuntimeLockSHA: sha("b"),
		DeviceProfileSHA: sha("c"), DatasetSHA: sha("d"),
		IssuerID: uuid.New().String(), RequiredChecks: []string{"quality"},
		CalibrationSHAs: []string{sha("e")}, TestFixtureSHAs: []string{sha("f")},
		EvidenceSHAs: []string{sha("1")}, LicenseCleared: true}
	path := filepath.Join(t.TempDir(), "locks.json")
	write := func() {
		body, err := json.Marshal(locks)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if expected, err := LoadLocks(path); err != nil ||
		expected.Backend != "cuda" || len(expected.RequiredChecks) != 1 {
		t.Fatal("valid locks rejected", err)
	}
	locks.TestFixtureSHAs = []string{sha("e")}
	write()
	if _, err := LoadLocks(path); err == nil {
		t.Fatal("calibration/test overlap accepted")
	}
	locks.TestFixtureSHAs = []string{sha("f")}
	write()
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocks(path); err == nil {
		t.Fatal("writable locks accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocks(link); err == nil {
		t.Fatal("symlinked locks accepted")
	}
}
