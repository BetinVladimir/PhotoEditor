package v2qualification

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// Canonical ── pure logic, no DB ─────────────────────────────────────────────

func TestCanonical_validAuthority(t *testing.T) {
	a, _, _ := mixedFixture(t)
	if e := a.Validate(a.Tenant, a.Project); e != nil {
		t.Fatal(e)
	}
	raw, e := a.Canonical()
	if e != nil || len(raw) == 0 {
		t.Fatalf("Canonical on valid authority failed: %v", e)
	}
	var check MixedAuthority
	if json.Unmarshal(raw, &check) != nil {
		t.Fatal("Canonical output is not valid JSON")
	}
}

func TestCanonical_invalidAuthority(t *testing.T) {
	a, _, _ := mixedFixture(t)
	a.Schema = 999
	if _, e := a.Canonical(); e == nil {
		t.Fatal("Canonical on invalid authority must return error")
	}
}

// Child ── operation not found ─────────────────────────────────────────────

func TestChild_notFound(t *testing.T) {
	a, _, _ := mixedFixture(t)
	if _, e := a.Child("nonexistent.op", "1", "cpu", ""); e == nil {
		t.Fatal("Child with missing operation must return ErrUnqualified")
	}
}

func TestChild_modelMismatch(t *testing.T) {
	a, _, _ := mixedFixture(t)
	// pick the compose child (cpu) but pass the wrong model
	if _, e := a.Child("render.compose", "1", "cpu", "wrong-model"); e == nil {
		t.Fatal("Child with wrong model must return ErrUnqualified")
	}
}

// Validate ── edge cases not yet covered ──────────────────────────────────

func TestValidate_nilTenant(t *testing.T) {
	a, _, _ := mixedFixture(t)
	if e := a.Validate(uuid.Nil, a.Project); e == nil {
		t.Fatal("Validate with nil tenant must return error")
	}
}

func TestValidate_wrongSchema(t *testing.T) {
	a, _, _ := mixedFixture(t)
	a.Schema = 2
	if e := a.Validate(a.Tenant, a.Project); e == nil {
		t.Fatal("Validate with schema != 1 must return error")
	}
}

func TestValidate_wrongScope(t *testing.T) {
	a, _, _ := mixedFixture(t)
	a.Scope = "renderer.staged.cpu"
	if e := a.Validate(a.Tenant, a.Project); e == nil {
		t.Fatal("Validate with wrong scope must return error")
	}
}

func TestValidate_tenantProjectMismatch(t *testing.T) {
	a, _, _ := mixedFixture(t)
	if e := a.Validate(uuid.New(), a.Project); e == nil {
		t.Fatal("Validate with different tenant must return error")
	}
}

// DecodeMixedAuthority ── extra JSON value, malformed ────────────────────

func TestDecodeMixedAuthority_extraValue(t *testing.T) {
	a, _, _ := mixedFixture(t)
	raw, _ := json.Marshal(a)
	extra := append(raw, []byte(`{"extra":1}`)...)
	if _, e := DecodeMixedAuthority(extra, a.Tenant, a.Project); e == nil {
		t.Fatal("DecodeMixedAuthority with trailing JSON value must return error")
	}
}

func TestDecodeMixedAuthority_malformed(t *testing.T) {
	if _, e := DecodeMixedAuthority([]byte(`{not json`), uuid.New(), uuid.New()); e == nil {
		t.Fatal("DecodeMixedAuthority with malformed JSON must return error")
	}
}

// RequireMixedQualificationSchema ── nil DB early return ───────────────────

func TestRequireMixedQualificationSchema_nilDB(t *testing.T) {
	if e := RequireMixedQualificationSchema(context.Background(), nil); e == nil {
		t.Fatal("nil DB must return error")
	}
}
