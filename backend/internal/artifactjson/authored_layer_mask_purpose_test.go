package artifactjson

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func TestAuthoredLayerMaskDocumentPurposeExact(t *testing.T) {
	tenant, run, owner, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	scope := Scope{Tenant: tenant, Run: run, Owner: owner, Purpose: "v26_authored_layer_mask"}
	got, err := DocumentKey(scope, id)
	want, wantErr := objectstore.DocumentKey(tenant, run, id)
	if err != nil || wantErr != nil || got != want {
		t.Fatalf("exact authored layer Params document refused: %v", err)
	}
	for _, purpose := range []string{"v26_authored_layer_mask_other", "v26_authored_layer_mask/1", "v26_authored_unknown", ""} {
		bad := scope
		bad.Purpose = purpose
		if _, err := DocumentKey(bad, id); err == nil {
			t.Fatal("unknown purpose accepted")
		}
	}
}

func TestAuthoredLayerMaskDocumentIdentityAndLeaseRefusal(t *testing.T) {
	scope := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "v26_authored_layer_mask"}
	id := uuid.New()
	cases := []Scope{scope, scope, scope, scope}
	cases[0].Tenant = uuid.Nil
	cases[1].Run = uuid.Nil
	cases[2].Owner = uuid.Nil
	cases[3].AssemblyLease = uuid.New()
	for _, bad := range cases {
		if _, err := DocumentKey(bad, id); err == nil {
			t.Fatal("invalid identity or foreign lease accepted")
		}
	}
	if _, err := DocumentKey(scope, uuid.Nil); err == nil {
		t.Fatal("zero artifact accepted")
	}
}
