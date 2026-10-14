package workflow

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPerspectivePreviewIntentRefusesChangedPlan(t *testing.T) {
	plan, err := CompilePerspectivePreviewPlan(perspectivePlanBundle(t), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePerspectivePreviewIntent(plan); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*PerspectivePreviewPlan){
		func(p *PerspectivePreviewPlan) { p.run = uuid.New() }, func(p *PerspectivePreviewPlan) { p.source.Fence = 0 },
		func(p *PerspectivePreviewPlan) { p.source.Principal.SessionID = uuid.Nil }, func(p *PerspectivePreviewPlan) { p.srgb = "invalid" },
		func(p *PerspectivePreviewPlan) { p.documents = p.documents[:1] }, func(p *PerspectivePreviewPlan) { p.documents[0].ParamsSHA = strings.Repeat("f", 64) },
	}
	for i, mutate := range mutations {
		bad := plan
		bad.documents = plan.Documents()
		mutate(&bad)
		if err := validatePerspectivePreviewIntent(bad); err == nil {
			t.Fatal("changed plan accepted", i)
		}
	}
}
func TestPerspectivePreviewIntentRefusesBeforeDatabase(t *testing.T) {
	plan, err := CompilePerspectivePreviewPlan(perspectivePlanBundle(t), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (PerspectivePreviewIntentStore{}).Save(context.Background(), plan); err == nil {
		t.Fatal("missing DB accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectivePreviewIntentStore{}).Save(ctx, plan); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
