package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredFinalProcessorCancellationAndUnconfiguredRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (*AuthoredFinalParentProcessor)(nil).ProcessOne(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (&AuthoredFinalParentProcessor{}).ProcessOne(context.Background()); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing concrete preparation accepted")
	}
}
func TestAuthoredFinalDeploymentRequiresPinnedPhysicalCatalogAndClosedAuthority(t *testing.T) {
	d := AuthoredFinalDeployment{SchemaVersion: 1, Tenants: []uuid.UUID{uuid.New()}, CalibrationPath: filepath.Join(t.TempDir(), "actual-approved-catalog.json"), CalibrationSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := d.validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "deployment.json")
	raw, _ := json.Marshal(d)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	pin := hex.EncodeToString(sum[:])
	got, err := LoadAuthoredFinalDeployment(path, pin)
	if err != nil || got.CalibrationSHA != d.CalibrationSHA || len(got.Tenants) != 1 {
		t.Fatal(err)
	}
	for _, name := range []string{"duplicate-tenant", "zero-tenant", "schema", "catalog-relative", "catalog-pin"} {
		t.Run(name, func(t *testing.T) {
			bad := d
			bad.Tenants = append([]uuid.UUID(nil), d.Tenants...)
			switch name {
			case "duplicate-tenant":
				bad.Tenants = append(bad.Tenants, bad.Tenants[0])
			case "zero-tenant":
				bad.Tenants[0] = uuid.Nil
			case "schema":
				bad.SchemaVersion = 2
			case "catalog-relative":
				bad.CalibrationPath = "catalog.json"
			case "catalog-pin":
				bad.CalibrationSHA = "invalid"
			}
			if bad.validate() == nil {
				t.Fatal("invalid deployment accepted")
			}
		})
	}
	if _, err := LoadAuthoredFinalDeployment(path, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("wrong physical pin accepted")
	}
	var unknown map[string]any
	json.Unmarshal(raw, &unknown)
	unknown["default_qualified"] = true
	hostile, _ := json.Marshal(unknown)
	os.WriteFile(path, hostile, 0600)
	sum = sha256.Sum256(hostile)
	if _, err := LoadAuthoredFinalDeployment(path, hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("unknown authority override accepted")
	}
}
