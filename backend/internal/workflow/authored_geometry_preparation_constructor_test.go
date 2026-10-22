package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAuthoredGeometryConstructorOwnsTenantGenerationInventory(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	r := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	tenants := []uuid.UUID{a, b}
	refs := map[uuid.UUID]v2input.Ref{a: r, b: r}
	copied, owned, e := ownAuthoredGeometryInventory(context.Background(), tenants, refs)
	if e != nil {
		t.Fatal(e)
	}
	tenants[0] = uuid.Nil
	delete(refs, a)
	if copied[0] != a || owned[a] != r {
		t.Fatal("caller mutation changed inventory")
	}
	copied[0] = b
	delete(owned, a)
	if len(refs) != 1 || tenants[0] != uuid.Nil {
		t.Fatal("returned inventory aliases caller")
	}
}
func TestAuthoredGeometryConstructorRejectsInvalidInventory(t *testing.T) {
	a := uuid.New()
	r := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	cases := []struct {
		tenants []uuid.UUID
		refs    map[uuid.UUID]v2input.Ref
	}{
		{nil, nil}, {[]uuid.UUID{uuid.Nil}, map[uuid.UUID]v2input.Ref{uuid.Nil: r}},
		{[]uuid.UUID{a, a}, map[uuid.UUID]v2input.Ref{a: r, uuid.New(): r}},
		{[]uuid.UUID{a}, map[uuid.UUID]v2input.Ref{uuid.New(): r}},
		{[]uuid.UUID{a}, map[uuid.UUID]v2input.Ref{a: {ArtifactID: uuid.Nil, SHA256: r.SHA256}}},
		{[]uuid.UUID{a}, map[uuid.UUID]v2input.Ref{a: {ArtifactID: r.ArtifactID, SHA256: strings.Repeat("A", 64)}}},
	}
	for i, c := range cases {
		if _, _, e := ownAuthoredGeometryInventory(context.Background(), c.tenants, c.refs); !errors.Is(e, v2input.ErrInvalid) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
}
func TestAuthoredGeometryConstructorCancellationAndMissingStores(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := NewAuthoredGeometryPreparationProcessor(ctx, nil, nil, nil, nil, "", "", "", nil, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	a := uuid.New()
	r := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	if p, e := NewAuthoredGeometryPreparationProcessor(context.Background(), nil, nil, nil, nil, "", "missing-no-IO", "", []uuid.UUID{a}, map[uuid.UUID]v2input.Ref{a: r}); e == nil || p != nil {
		t.Fatal("missing physical/SQL stores accepted")
	}
}

func TestAuthoredGeometryNativeOnlyConfigurationCannotSilentlyDisable(t *testing.T) {
	for _, key := range []string{"PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_DEPLOYMENT_SHA256", "PHOTO_AUTHORED_GEOMETRY_PREPARATION_API_DATABASE_URL", "PHOTO_AUTHORED_NATIVE_DEPLOYMENT", "PHOTO_AUTHORED_NATIVE_DEPLOYMENT_SHA256"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"PHOTO_AUTHORED_NATIVE_DEPLOYMENT", "PHOTO_AUTHORED_NATIVE_DEPLOYMENT_SHA256"} {
		t.Setenv(key, "configured")
		p, close, e := buildAuthoredGeometryPreparation(context.Background(), nil, nil, "")
		if !errors.Is(e, ErrAuthoredGeometryInstalledCaptureRequired) || p != nil || close != nil {
			t.Fatalf("%s: %v", key, e)
		}
		t.Setenv(key, "")
	}
}
