package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestPerspectiveQualityIntentValidationAndMissingDB(t *testing.T) {
	good, err := CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePerspectiveQualityIntent(good); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*PerspectiveQualityPlan){
		func(p *PerspectiveQualityPlan) { p.source.Tenant = uuid.Nil },
		func(p *PerspectiveQualityPlan) { p.source.Principal.SessionID = uuid.Nil },
		func(p *PerspectiveQualityPlan) { p.source.Base.Source.SHA256 = "invalid" },
		func(p *PerspectiveQualityPlan) { p.source.Workflow = uuid.New() },
		func(p *PerspectiveQualityPlan) { p.source.Producer = uuid.New() },
		func(p *PerspectiveQualityPlan) { p.source.Fence = 0 },
		func(p *PerspectiveQualityPlan) { p.run = uuid.New() },
	} {
		bad := good
		alter(&bad)
		if validatePerspectiveQualityIntent(bad) == nil {
			t.Fatal("invalid intent accepted")
		}
	}
	if _, err := (PerspectiveQualityIntentStore{}).Save(context.Background(), good); err == nil {
		t.Fatal("missing database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveQualityIntentStore{}).Save(ctx, good); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestPerspectiveQualityInputsCannotAliasOrLoseDigest(t *testing.T) {
	good, err := CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*PerspectiveQualityPlan){
		func(p *PerspectiveQualityPlan) { p.inputs[1] = p.inputs[0] },
		func(p *PerspectiveQualityPlan) { p.inputs[1].SHA256 = "invalid" },
	} {
		bad := good
		bad.inputs = good.Inputs()
		alter(&bad)
		if validatePerspectiveQualityIntent(bad) == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestPerspectiveQualityQuotaRefusesUnavailableAndCancelledScope(t *testing.T) {
	plan, err := CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	store := PerspectiveQualityIntentStore{}
	if ok, err := store.Reserve(context.Background(), plan); ok || err == nil {
		t.Fatal("missing reservation database accepted")
	}
	if ok, err := store.Release(context.Background(), uuid.Nil, plan.Run()); ok || err == nil {
		t.Fatal("empty tenant accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Reserve(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.Release(ctx, uuid.New(), plan.Run()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if PerspectiveQualityReservedBytes < 2*1048576 {
		t.Fatal("report/Params quota missing")
	}
}
