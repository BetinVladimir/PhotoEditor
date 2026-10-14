package interchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func exportModelLockFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	model := exportModelEntry{
		ID: "mobilesam", Architecture: "vit", WeightID: "weights", Repository: "release",
		Revision: "revision1", PreprocessingID: "rgb", PackagesLockHash: rawSHA([]byte("packages")),
		CodeLicense: "MIT", WeightsLicense: "MIT", Notices: []string{},
		RedistributionPolicy: "allowed", ApprovedForInstall: true,
		Files: []exportModelFile{{Path: "model.pt", SHA: rawSHA([]byte("weight")), Bytes: 6, URL: "https://github.com/model.pt"}},
		Backends: map[string]exportModelBackend{
			"cpu":  {Status: "untested", Reason: "pending"},
			"cuda": {Status: "untested", Reason: "pending"},
			"mps":  {Status: "untested", Reason: "pending"},
		},
	}
	payload := struct {
		Schema int                `json:"schema_version"`
		Models []exportModelEntry `json:"models"`
	}{1, []exportModelEntry{model}}
	lockID, err := canonical.Hash(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(exportModelLock{Schema: 1, LockID: lockID, Models: payload.Models})
	if err != nil {
		t.Fatal(err)
	}
	return raw, lockID
}

func TestExportModelLockRequiresPinnedCanonicalFileAndInternalID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.lock.json")
	raw, lockID := exportModelLockFixture(t)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadExportModelLockSHA(context.Background(), path, rawSHA(raw)); err != nil || got != lockID || got == rawSHA(raw) {
		t.Fatal("model lock ID unavailable", err)
	}
	if got, weights, err := LoadExportModelLockWeights(context.Background(), path, rawSHA(raw)); err != nil || got != lockID || weights["mobilesam"] != "weights" {
		t.Fatal("authenticated model weight inventory unavailable", err)
	}
	if got, err := LoadExportModelLockSHA(context.Background(), path, rawSHA([]byte("foreign"))); err == nil || got != "" {
		t.Fatal("foreign file pin accepted")
	}
	link := filepath.Join(filepath.Dir(path), "linked.lock.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadExportModelLockSHA(context.Background(), link, rawSHA(raw)); err == nil || got != "" {
		t.Fatal("linked lock accepted")
	}
	var unapproved exportModelLock
	if err := decodeImportDocument(raw, &unapproved); err != nil {
		t.Fatal(err)
	}
	unapproved.Models[0].ApprovedForInstall = false
	unapproved.LockID, err = canonical.Hash(struct {
		Schema int                `json:"schema_version"`
		Models []exportModelEntry `json:"models"`
	}{unapproved.Schema, unapproved.Models})
	if err != nil {
		t.Fatal(err)
	}
	unapprovedRaw, err := canonical.Marshal(unapproved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, unapprovedRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, weights, err := LoadExportModelLockWeights(context.Background(), path, rawSHA(unapprovedRaw)); err != nil || len(weights) != 0 {
		t.Fatal("unapproved model exposed as installable", err)
	}
	invalid := append([]byte(nil), raw...)
	var changed exportModelLock
	if err := decodeImportDocument(invalid, &changed); err != nil {
		t.Fatal(err)
	}
	changed.LockID = rawSHA([]byte("wrong"))
	invalid, err := canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadExportModelLockSHA(context.Background(), path, rawSHA(invalid)); err == nil || got != "" {
		t.Fatal("wrong internal lock ID accepted")
	}
}
