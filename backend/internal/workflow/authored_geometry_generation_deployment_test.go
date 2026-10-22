package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

func geometryDeploymentFixture(t *testing.T) (map[string]any, uuid.UUID, v2input.Ref) {
	t.Helper()
	tenant := uuid.New()
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	return map[string]any{"schema_version": 1, "contracts_sha": v2render.AuthoredGeometrySchemasSHA, "tenants": []any{map[string]any{"tenant_id": tenant.String(), "generation": map[string]any{"artifact_id": ref.ArtifactID.String(), "sha256": ref.SHA256}}}}, tenant, ref
}
func geometryDeploymentRaw(t *testing.T, m any) []byte {
	t.Helper()
	raw, e := canonical.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestAuthoredGeometryGenerationDeploymentOwnsSelection(t *testing.T) {
	m, tenant, ref := geometryDeploymentFixture(t)
	raw := geometryDeploymentRaw(t, m)
	d, e := parseAuthoredGeometryGenerationDeployment(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	for i := range raw {
		raw[i] = 0
	}
	a, b := d.Inventory()
	if len(a) != 1 || a[0] != tenant || b[tenant] != ref {
		t.Fatal("wrong actual selection")
	}
	a[0] = uuid.Nil
	delete(b, tenant)
	a, b = d.Inventory()
	if a[0] != tenant || b[tenant] != ref {
		t.Fatal("caller mutated private deployment")
	}
}
func TestAuthoredGeometryGenerationDeploymentClosedSchema(t *testing.T) {
	cases := map[string]func(map[string]any){
		"case-alias": func(m map[string]any) { m["SCHEMA_VERSION"] = m["schema_version"]; delete(m, "schema_version") },
		"unknown":    func(m map[string]any) { m["extra"] = true }, "version": func(m map[string]any) { m["schema_version"] = 2 }, "old-generation": func(m map[string]any) { m["contracts_sha"] = strings.Repeat("b", 64) }, "missing": func(m map[string]any) { delete(m, "tenants") }, "null": func(m map[string]any) { m["tenants"] = nil }, "empty": func(m map[string]any) { m["tenants"] = []any{} },
		"duplicate":     func(m map[string]any) { a := m["tenants"].([]any); m["tenants"] = append(a, a[0]) },
		"foreign-field": func(m map[string]any) { m["tenants"].([]any)[0].(map[string]any)["other"] = true },
		"zero-tenant":   func(m map[string]any) { m["tenants"].([]any)[0].(map[string]any)["tenant_id"] = uuid.Nil.String() },
		"missing-ref":   func(m map[string]any) { delete(m["tenants"].([]any)[0].(map[string]any), "generation") },
		"null-entry":    func(m map[string]any) { m["tenants"] = []any{nil} },
		"null-ref":      func(m map[string]any) { m["tenants"].([]any)[0].(map[string]any)["generation"] = nil },
		"upper-sha": func(m map[string]any) {
			m["tenants"].([]any)[0].(map[string]any)["generation"].(map[string]any)["sha256"] = strings.Repeat("A", 64)
		},
		"unknown-ref": func(m map[string]any) {
			m["tenants"].([]any)[0].(map[string]any)["generation"].(map[string]any)["extra"] = 1
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, _, _ := geometryDeploymentFixture(t)
			if _, e := parseAuthoredGeometryGenerationDeployment(context.Background(), geometryDeploymentRaw(t, m)); e != nil {
				t.Fatal("positive baseline", e)
			}
			mutate(m)
			if _, e := parseAuthoredGeometryGenerationDeployment(context.Background(), geometryDeploymentRaw(t, m)); !errors.Is(e, v2input.ErrInvalid) {
				t.Fatalf("hostile accepted: %v", e)
			}
		})
	}
}
func TestAuthoredGeometryGenerationDeploymentPhysicalFile(t *testing.T) {
	m, _, _ := geometryDeploymentFixture(t)
	raw := geometryDeploymentRaw(t, m)
	h := sha256.Sum256(raw)
	pin := hex.EncodeToString(h[:])
	path := filepath.Join(t.TempDir(), "deployment.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadAuthoredGeometryGenerationDeployment(context.Background(), path, pin); e != nil {
		t.Fatal("positive", e)
	}
	if _, e := ReadAuthoredGeometryGenerationDeployment(context.Background(), path, strings.Repeat("0", 64)); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("SHA", e)
	}
	if e := os.Chmod(path, 0640); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadAuthoredGeometryGenerationDeployment(context.Background(), path, pin); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("private permissions", e)
	}
	if e := os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	link := path + ".link"
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadAuthoredGeometryGenerationDeployment(context.Background(), link, pin); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("symlink", e)
	}
	if e := os.WriteFile(path, append(raw, ' '), 0600); e != nil {
		t.Fatal(e)
	}
	h = sha256.Sum256(append(raw, ' '))
	if _, e := ReadAuthoredGeometryGenerationDeployment(context.Background(), path, hex.EncodeToString(h[:])); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("noncanonical", e)
	}
}
func TestAuthoredGeometryGenerationDeploymentCancellationAndBounds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := ReadAuthoredGeometryGenerationDeployment(ctx, "missing", "bad"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := parseAuthoredGeometryGenerationDeployment(context.Background(), make([]byte, (1<<20)+1)); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := NewAuthoredGeometryPreparationFromDeployment(ctx, nil, nil, nil, nil, "", "", "", "missing", ""); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
