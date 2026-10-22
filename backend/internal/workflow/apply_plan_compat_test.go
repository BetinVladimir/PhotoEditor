package workflow

import (
	"encoding/json"
	"testing"
)

func TestFrozenApplyPlanPreservesAcceptedStages(t *testing.T) {
	old := []ApplyV4Stage{itemStage("resolve", "style.resolve", "resolved_style"), itemStage("color", "image.color", "working_image"), itemStage("series_measure", "series.measure", "series_stats"), {"series_balance", "series.balance", "series_corrections", "aggregate"}, itemStage("local", "image.local", "working_image"), itemStage("quality", "quality.check", "quality_report")}
	cfg := json.RawMessage(`{"style_config":{"mode":"fixed","extensions":{"series":{"enabled":true}}}}`)
	plan, reason, err := applyCompatiblePlan(applyFrozenPlan{Version: 1, Stages: old, Config: cfg}, 128)
	if err != nil || reason != "" {
		t.Fatalf("runnable frozen plan rejected: %s %v", reason, err)
	}
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(plan)
	if string(a) != string(b) {
		t.Fatal("accepted plan rewritten")
	}
	_, reason, err = applyCompatiblePlan(applyFrozenPlan{Version: 1, Stages: old, Config: cfg}, 129)
	if err != nil || reason != "APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT" {
		t.Fatal("unsafe legacy fan-in allowed")
	}
}

func TestFrozenApplyCullingNeedsPersistedHumanBarrier(t *testing.T) {
	cfg := json.RawMessage(`{"style_config":{"mode":"fixed","extensions":{"culling":{"enabled":true,"never_delete":true,"eye_policy":"review"}}}}`)
	_, reason, err := applyCompatiblePlan(applyFrozenPlan{Version: 1, Config: cfg}, 1)
	if err != nil || reason != "APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING" {
		t.Fatal("silent legacy culling allowed")
	}
	plan, reason, err := applyCompatiblePlan(applyFrozenPlan{Version: 2, Config: cfg}, 1)
	if err != nil || reason != "" {
		t.Fatal(err)
	}
	found := false
	for _, stage := range plan {
		found = found || stage.Phase == "cull_review"
	}
	if !found {
		t.Fatal("new plan omits photographer barrier")
	}
}

func TestFrozenApplyCompatibilityUsesEffectiveOverride(t *testing.T) {
	cfg := json.RawMessage(`{"style_config":{"mode":"fixed","extensions":{}},"job_overrides":{"modules":{"extensions":{"culling":{"enabled":true,"never_delete":true,"eye_policy":"review"}}}}}`)
	_, reason, err := applyCompatiblePlan(applyFrozenPlan{Version: 1, Config: cfg}, 2)
	if err != nil || reason != "APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING" {
		t.Fatal("job culling override ignored")
	}
}

func TestSeriesFanInFollowsSavedStages(t *testing.T) {
	cases := []struct {
		name   string
		lease  ApplyLease
		direct bool
	}{
		{"legacy graph", ApplyLease{GraphVersion: 3}, true},
		{"old direct", ApplyLease{GraphVersion: 4, PlanContractVersion: 1}, true},
		{"old saved packs", ApplyLease{GraphVersion: 4, PlanContractVersion: 1, FrozenPlan: []ApplyV4Stage{{Phase: "series_pack"}}}, false},
		{"new packs", ApplyLease{GraphVersion: 4, PlanContractVersion: 2}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if applyUsesDirectSeries(tc.lease) != tc.direct {
				t.Fatal("saved fan-in was changed")
			}
		})
	}
}

func itemStage(phase, operation, role string) ApplyV4Stage {
	return ApplyV4Stage{Phase: phase, Operation: operation, OutputRole: role, Cardinality: "item"}
}
