package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeDefectDraftPlan struct {
	seal, key string
	prepared  nativeDefectTaskPreparation
	shell     v2input.DraftShell
}

// Owned blocked graph with a distinct native-input marker and exact inventory.
// Registration retains blocked state; activation requires separate admission.
func (s SQLTransferSourceReader) compileNativeDefectDraftPlan(ctx context.Context, who auth.Identity, p nativeDefectTaskPreparation, reserved int64) (nativeDefectDraftPlan, error) {
	var zero nativeDefectDraftPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	task, err := p.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	// Explicit field names preserve existing canonical resource profile.
	var resources struct {
		Backend  string `json:"backend"`
		Estimate struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &resources) != nil || resources.Backend != "cpu" || resources.Estimate.Output < 1 || resources.Estimate.Output > 9007199254740991-(8<<20) || reserved < resources.Estimate.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeDefectTask(ctx, who, p.dependencies, p.qualification, p.params, p.additional, resources.Estimate.RAM, resources.Estimate.VRAM, resources.Estimate.Scratch, resources.Estimate.Output)
	if err != nil {
		return zero, err
	}
	owned.legacyQueueOmitted = p.legacyQueueOmitted
	current, err := owned.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(current, task) || owned.entries != p.entries || owned.sizes != p.sizes {
		return zero, v2input.ErrConflict
	}
	shell, run, err := s.assembleNativeDefectDraftShell(ctx, who, owned, current, reserved)
	if err != nil {
		return zero, err
	}
	plan := nativeDefectDraftPlan{key: "defect-prep:" + run.String(), prepared: owned, shell: shell}
	plan.seal, err = nativeDefectDraftDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}

func nativeDefectDraftDigest(p nativeDefectDraftPlan) (string, error) {
	pins := []nativeSkinCompletedFile{}
	for _, entry := range append([]nativeDefectInputPin{{"Params", p.prepared.params}}, p.prepared.additional...) {
		pins = append(pins, nativeSkinCompletedFile{Role: entry.contract, Ref: entry.pin.ref, Key: entry.pin.object.Key, Version: entry.pin.object.VersionID, Bytes: entry.pin.object.Bytes, Media: entry.pin.object.ContentType})
	}
	return canonical.Hash(map[string]any{"shell": p.shell, "key": p.key, "dependencies": p.prepared.dependencies.seal, "qualification": p.prepared.qualification.report.expected, "qualification_pin": map[string]any{"ref": p.prepared.qualification.report.report.ref, "object": p.prepared.qualification.report.report.object, "backend": p.prepared.qualification.report.report.backend, "contract": p.prepared.qualification.report.report.contract}, "qualification_metrics": p.prepared.qualification.report.metrics, "policy": p.prepared.qualification.policy.body, "costs": p.prepared.qualification.costs, "entries": p.prepared.entries, "sizes": p.prepared.sizes, "pins": pins})
}

func verifyNativeDefectDraftSeal(ctx context.Context, p nativeDefectDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeDefectDraftDigest(p)
	if err != nil {
		return err
	}
	if digest != p.seal {
		return v2input.ErrConflict
	}
	if p.prepared.dependencies.preparation.person != nil {
		dependency, err := nativeDefectDependencyDigest(p.prepared.dependencies)
		if err != nil {
			return err
		}
		if dependency != p.prepared.dependencies.seal {
			return v2input.ErrConflict
		}
	}
	return nil
}

// Pure canonical config/shell assembly; callers validate their distinct producer
// authority before invoking it. Its data argument is not an admission proof.
func (s SQLTransferSourceReader) assembleNativeDefectDraftShell(ctx context.Context, who auth.Identity, owned nativeDefectTaskPreparation, current v2input.DraftTask, reserved int64) (v2input.DraftShell, uuid.UUID, error) {
	parent := owned.dependencies.preparation.landmarksPlan.detector
	r := parent.request
	run, workflow, err := defectPreparationIDs(r.RunID)
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	id, err := v2input.TaskID(workflow, "defects.propose", 0)
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	if current.ID != id || current.Operation != "defects.propose" || current.ResourceClass != "cpu" {
		return v2input.DraftShell{}, uuid.Nil, v2input.ErrConflict
	}
	f := owned.dependencies.preparation
	metadata, err := v2input.ResolveSemanticSourceMetadata(r.Intent.Reference, []byte(f.landmarks.detector.sourceBody))
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	direct := []v2input.Ref{owned.params.ref, r.Intent.Reference.Source, metadata.Inputs.Pixels}
	sort.Slice(direct, func(i, j int) bool { return bytes.Compare(direct[i].ArtifactID[:], direct[j].ArtifactID[:]) < 0 })
	recovery, err := nativePlanRecoveryFromTask(current, &owned.qualification.report.report.ref, owned.qualification.costs, reserved)
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	configValue := map[string]any{"project_id": s.ProjectID, "v27_defect_native_prepare": map[string]any{"recovery": recovery, "schema_version": 1, "operation": "defects.propose", "parent_run_id": r.RunID, "request_sha": r.RequestSHA, "auth_session_id": who.SessionID, "source": r.Intent.Reference, "intent": parent.published.Ref, "snapshot": parent.anchor.snapshotRef, "params_document": uuid.NewSHA1(r.RunID, []byte("semantic.defect.params:reference")), "params": owned.params.ref, "task_id": current.ID, "input_digest": current.InputDigest, "policy_sha": parent.shell.PolicySHA, "dependency_sha": owned.dependencies.seal, "input_entries": json.RawMessage(owned.entries), "direct_inputs": direct, "landmarks_run_id": f.landmarksPlan.shell.Run, "landmarks_task_id": f.landmarksPlan.shell.Tasks[0].ID, "landmarks_result_id": f.landmarks.catalog.ResultID, "landmarks_outputs": f.landmarks.catalog.Outputs, "landmarks_params": f.landmarks.catalog.Params, "skin_run_id": f.skinPlan.shell.Run, "skin_task_id": f.skinPlan.shell.Tasks[0].ID, "skin_result_id": f.skin.catalog.ResultID, "skin_outputs": f.skin.catalog.Outputs, "skin_params": f.skin.catalog.Params}}
	if !owned.legacyQueueOmitted {
		configValue["queue_class"] = "batch"
	}
	config, err := canonical.Marshal(configValue)
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	// Keep legacy config bytes unchanged when there is no person authority.
	if f.person != nil {
		locator, err := nativeDefectPersonRecoveryFromProof(ctx, *f.person)
		if err != nil {
			return v2input.DraftShell{}, uuid.Nil, err
		}
		var envelope map[string]json.RawMessage
		var stage map[string]json.RawMessage
		if json.Unmarshal(config, &envelope) != nil || json.Unmarshal(envelope["v27_defect_native_prepare"], &stage) != nil {
			return v2input.DraftShell{}, uuid.Nil, v2input.ErrInvalid
		}
		stage["person_input"], err = canonical.Marshal(locator)
		if err != nil {
			return v2input.DraftShell{}, uuid.Nil, err
		}
		envelope["v27_defect_native_prepare"], err = canonical.Marshal(stage)
		if err != nil {
			return v2input.DraftShell{}, uuid.Nil, err
		}
		config, err = canonical.Marshal(envelope)
		if err != nil {
			return v2input.DraftShell{}, uuid.Nil, err
		}
	}
	sha, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return v2input.DraftShell{}, uuid.Nil, err
	}
	shell := v2input.DraftShell{Tenant: r.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: r.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: parent.shell.PolicySHA, ConfigSHA: sha, Config: config, Tasks: []v2input.DraftTask{current}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	return shell, run, ctx.Err()
}
