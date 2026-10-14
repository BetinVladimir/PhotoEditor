package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestPlanSourcesRejectPartialDuplicateAndUnrelatedObjects(t *testing.T) {
	sources := PlanSources{RunID: uuid.New(), TaskID: uuid.New(), ResultID: uuid.New(), Revision: 1}
	for _, contract := range []string{"SemanticPlan", "Params", "InputSnapshot", "EditIntent", "Calibration", "TransferCandidate"} {
		sources.Objects = append(sources.Objects, PlanSource{Contract: contract, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 2, Bucket: "fixture", Key: "key", Version: "version", MediaType: "application/json"})
	}
	if err := ValidatePlanSources(sources); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PlanSources){
		func(p *PlanSources) { p.Objects = p.Objects[:5] },
		func(p *PlanSources) { p.Objects[5].Contract = "WorkingImage" },
		func(p *PlanSources) { p.Objects[5].Ref = p.Objects[0].Ref },
		func(p *PlanSources) { p.Objects[5].Version = "null" },
		func(p *PlanSources) { p.Objects[5].Bytes = 8<<20 + 1 },
		func(p *PlanSources) { p.Objects[0].Contract = "Params" },
		func(p *PlanSources) { p.ResultID = uuid.Nil },
	} {
		owned := sources
		owned.Objects = append([]PlanSource(nil), sources.Objects...)
		mutate(&owned)
		if ValidatePlanSources(owned) == nil {
			t.Fatal("invalid sources accepted")
		}
	}
}
