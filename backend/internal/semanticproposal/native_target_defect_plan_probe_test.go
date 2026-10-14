package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func probeNativeTargetDefectPlan(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, prepared nativeTargetDefectTaskPreparation, reserved int64) nativeTargetDefectDraftPlan {
	t.Helper()
	plan, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, prepared, reserved)
	if err != nil {
		t.Fatal("target defect plan", err)
	}
	var queueConfig map[string]json.RawMessage
	if json.Unmarshal(plan.shell.Config, &queueConfig) != nil || string(queueConfig["queue_class"]) != `"batch"` { t.Fatal("target defect queue missing") }
	oldPrepared := prepared
	oldPrepared.legacyQueueOmitted = true
	oldPlan, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, oldPrepared, reserved)
	if err != nil { t.Fatal("historical target defect compile", err) }
	var oldConfig map[string]json.RawMessage
	if json.Unmarshal(oldPlan.shell.Config, &oldConfig) != nil { t.Fatal("historical target defect config") }
	if _, present := oldConfig["queue_class"]; present { t.Fatal("historical target defect queue fabricated") }
	oldReplay, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, oldPlan.prepared, reserved)
	if err != nil || oldReplay.seal != oldPlan.seal || string(oldReplay.shell.Config) != string(oldPlan.shell.Config) { t.Fatal("historical target defect replay changed", err) }
	again, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, prepared, reserved)
	if err != nil || again.seal != plan.seal {
		t.Fatal("target defect plan replay", err)
	}
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		t.Fatal("target defect plan seal", err)
	}
	if plan.anchor.stage.Operation != "defects.propose" || plan.anchor.entry.Namespace != prepared.preparation.personPlan.anchor.entry.Namespace || len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 || plan.shell.Tasks[0].ResourceClass != "cpu" {
		t.Fatal("target defect plan scope")
	}
	var config struct {
		Stage struct {
			Recovery nativePlanRecoveryMetadata    `json:"recovery"`
			Parent   uuid.UUID                     `json:"parent_run_id"`
			Source   v2input.SessionVersionBinding `json:"source"`
			Direct   []v2input.Ref                 `json:"direct_inputs"`
		} `json:"v27_target_defect_prepare"`
	}
	if json.Unmarshal(plan.shell.Config, &config) != nil || config.Stage.Parent != plan.anchor.request.RunID || config.Stage.Source != plan.anchor.entry.Binding || len(config.Stage.Direct) != 3 || config.Stage.Recovery.Report == nil || *config.Stage.Recovery.Report != prepared.qualification.report.report.ref || config.Stage.Recovery.VRAM != 0 || config.Stage.Recovery.Reserved != reserved {
		t.Fatal("target defect saved config")
	}
	for _, kind := range []string{"namespace", "stage", "config", "shell", "budget", "task", "qualification_costs", "qualification_metrics", "prepared_params", "person_pin", "entries"} {
		changed := plan
		switch kind {
		case "namespace":
			changed.anchor.entry.Namespace = uuid.New()
		case "stage":
			changed.anchor.stage.Operation = "mask.person"
		case "config":
			changed.shell.Config = json.RawMessage(`{}`)
		case "shell":
			changed.shell.Actor = uuid.New()
		case "budget":
			changed.shell.Bytes++
		case "task":
			changed.shell.Tasks = append([]v2input.DraftTask(nil), plan.shell.Tasks...)
			changed.shell.Tasks[0].InputDigest = "changed"
		case "qualification_costs":
			changed.prepared.qualification.costs.RAMBytes++
		case "qualification_metrics":
			changed.prepared.qualification.report.metrics.PeakRSSBytes++
		case "prepared_params":
			changed.prepared.preparation.params.params = "{}"
		case "person_pin":
			changed.prepared.personPixels.object.VersionID = "changed"
		case "entries":
			changed.prepared.entries = "[]"
		}
		if verifyNativeTargetDefectPlanSeal(ctx, changed) == nil {
			t.Fatal("target defect plan substitution accepted", kind)
		}
	}
	for _, kind := range []string{"task_seal", "params", "qualification", "inventory"} {
		changed := prepared
		switch kind {
		case "task_seal":
			changed.seal = "changed"
		case "params":
			changed.params.ref.ArtifactID = uuid.New()
		case "qualification":
			changed.qualification.report.metrics.PeakRSSBytes++
		case "inventory":
			changed.sizes = "[]"
		}
		if _, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, changed, reserved); err == nil {
			t.Fatal("target defect compiler substitution accepted", kind)
		}
	}
	if _, err := p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, prepared, 1); err == nil {
		t.Fatal("target defect inadequate reserve accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := verifyNativeTargetDefectPlanSeal(canceled, plan); err == nil {
		t.Fatal("target defect canceled seal accepted")
	}
	t.Log("V27_TARGET_DEFECT_PLAN_REPLAY_SUBSTITUTION_OK")
	return plan
}
