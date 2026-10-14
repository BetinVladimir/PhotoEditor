package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveLinesQuotaRejectsCorruptedPlanBeforeDatabase(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	for _, alter := range []func(*PerspectiveLinesPlan){
		func(p *PerspectiveLinesPlan) { p.task = uuid.New() },
		func(p *PerspectiveLinesPlan) { p.params = []byte(`{}`) },
		func(p *PerspectiveLinesPlan) { p.profile = []byte(`{}`) },
		func(p *PerspectiveLinesPlan) { p.scope.Tenant = uuid.Nil },
	} {
		plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
		if err != nil {
			t.Fatal(err)
		}
		alter(&plan)
		ok, err := (PerspectiveLinesIntentStore{}).Reserve(context.Background(), plan)
		if ok || !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("corrupt quota request accepted", ok, err)
		}
	}
}
func TestPerspectiveLinesQuotaCancellationAndMissingIdentity(t *testing.T) {
	store := PerspectiveLinesIntentStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := store.Reserve(ctx, PerspectiveLinesPlan{}); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	if ok, err := store.Release(ctx, uuid.New(), uuid.New()); ok || !errors.Is(err, context.Canceled) {
		t.Fatal(ok, err)
	}
	for _, pair := range [][2]uuid.UUID{{uuid.Nil, uuid.New()}, {uuid.New(), uuid.Nil}, {uuid.New(), uuid.New()}} {
		if ok, err := store.Release(context.Background(), pair[0], pair[1]); ok || !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(ok, err)
		}
	}
}
