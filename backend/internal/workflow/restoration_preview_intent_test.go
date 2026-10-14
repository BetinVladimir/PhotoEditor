package workflow

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestRestorationPreviewIntentRequiresExactFourTaskPlan(t *testing.T) {
	plan := restorationPreviewFixturePlan(t)
	if err := validateRestorationPreviewIntent(plan); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RestorationPreviewPlan){
		func(p *RestorationPreviewPlan) { p.run = uuid.New() },
		func(p *RestorationPreviewPlan) { p.docs[0].Side = "after" },
		func(p *RestorationPreviewPlan) { p.docs[1].Variant = "preview1600" },
		func(p *RestorationPreviewPlan) { p.docs[2].ID = p.docs[3].ID },
		func(p *RestorationPreviewPlan) { p.docs[3].ParamsSHA = "" },
		func(p *RestorationPreviewPlan) { p.docs[0].Inputs[0] = p.docs[2].Inputs[0] },
	} {
		bad := plan
		for i := range bad.docs {
			bad.docs[i].Inputs = append([]review.ArtifactRef(nil), plan.docs[i].Inputs...)
		}
		mutate(&bad)
		if validateRestorationPreviewIntent(bad) == nil {
			t.Fatal("mutated preview plan accepted")
		}
	}
}
