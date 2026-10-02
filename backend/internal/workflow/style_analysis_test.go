package workflow

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestStyleAnalysisPlanIsDeterministicAndBindsRuntime(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	lease := StyleAnalysisLease{RunID: run}
	pair := uuid.New()
	input := review.ArtifactRef{ID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	params := []byte(`{"pair_id":"` + pair.String() + `"}`)
	lock := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	a := makeStylePlan(lease, registry, "color_fit", 3, &pair, "pair.fit.color", "mps", []review.ArtifactRef{input}, nil, params, 4<<30, 1<<30, 2<<30, 8<<20, 12000000, lock)
	b := makeStylePlan(lease, registry, "color_fit", 3, &pair, "pair.fit.color", "mps", []review.ArtifactRef{input}, nil, params, 4<<30, 1<<30, 2<<30, 8<<20, 12000000, lock)
	if a.plan.ID != b.plan.ID || a.task.ID != a.plan.ID || a.plan.ParamsSHA == "" || a.task.ResourceClass != "gpu" {
		t.Fatal("unstable style plan")
	}
	var profile struct {
		Estimate ResourceEstimate `json:"resource_estimate"`
	}
	if err = json.Unmarshal(a.task.ExecutionProfile, &profile); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(a.task.ExecutionProfile, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["backend"]) != "\"mps\"" || string(raw["model_lock_id"]) != "\""+lock+"\"" || profile.Estimate.VRAMBytes != 1<<30 {
		t.Fatal("runtime binding missing")
	}
}

func TestWorkingInputsDeclareNestedPixelArtifacts(t *testing.T) {
	manifest := review.ArtifactRef{ID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	alpha := review.ArtifactRef{ID: uuid.New(), SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	got := workingInputs(manifest, review.WorkingImage{Pixels: pixels, Alpha: &alpha})
	if len(got) != 3 || got[0] != manifest || got[1] != pixels || got[2] != alpha {
		t.Fatalf("nested artifacts are not declared: %#v", got)
	}
}
