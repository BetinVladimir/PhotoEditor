package v2draft

import (
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestCorrectionPlanRequiresExactCurrentBinding(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	item := map[string]any{"binding": binding, "ordinal": 0,
		"delta": map[string]any{"ev": 0.25, "log_r": 0, "log_g": 0,
			"log_b": 0, "saturation_delta": 0, "retouch_delta": 0},
		"disposition": "apply", "reason": nil}
	plan := map[string]any{"schema_version": 1,
		"snapshot_digest": strings.Repeat("c", 64), "items": []any{item}}
	body, err := canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorrectionPlan(body, binding); err != nil {
		t.Fatal(err)
	}
	delta, err := CurrentCorrection(body, binding)
	if err != nil || delta.EV != 0.25 {
		t.Fatalf("wrong current correction: %+v, %v", delta, err)
	}
	item["disposition"] = "manual_locked"
	body, err = canonical.Marshal(plan)
	if err != nil || ValidateCorrectionPlan(body, binding) == nil {
		t.Fatal("accepted nonzero locked correction")
	}
	item["disposition"] = "apply"
	binding.AssetVersionID = uuid.New()
	if err := ValidateCorrectionPlan(body, binding); err == nil {
		t.Fatal("accepted plan without current asset")
	}
	item["ordinal"] = 1
	body, err = canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorrectionPlan(body, binding); err == nil {
		t.Fatal("accepted noncontiguous ordinals")
	}
}

func TestCorrectionVectorSumRejectsOutOfRangeResult(t *testing.T) {
	first := CorrectionVector{EV: 0.75, LogR: 0.1}
	second := CorrectionVector{EV: 0.26, LogR: 0.12}
	if _, err := SumCorrectionVectors(first, second); err == nil {
		t.Fatal("accepted an effective exposure over the contract bound")
	}
	second.EV = -0.25
	got, err := SumCorrectionVectors(first, second)
	if err != nil || got.EV != 0.5 || math.Abs(got.LogR-0.22) > 1e-12 {
		t.Fatalf("wrong bounded sum: %+v, %v", got, err)
	}
}
