package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"time"
)

// OperatorInteractiveMask is an explicitly approved user request, not a mask
// predicted in advance by the corpus or an authenticated persistent Override.
// Params must contain the actual user points/manual vectors. The current
// production operation validates their closed typed ABI before any output.
type OperatorInteractiveMask struct {
	Uses        []OperatorMaskUse    `json:"uses"`
	Task        uuid.UUID            `json:"task_id"`
	Item        uuid.UUID            `json:"item_id"`
	Kind        string               `json:"kind"`
	Params      json.RawMessage      `json:"params"`
	Declared    []review.ArtifactRef `json:"declared_inputs"`
	Backend     string               `json:"backend"`
	RuntimeLock string               `json:"runtime_lock"`
}

// MeasureInteractiveMask reuses the normal coordinator's pure compiler and
// actual compute. It records physical output evidence only. No SQL admission,
// canonical success, accepted override or renderer qualification is created.
func MeasureInteractiveMask(ctx context.Context, ledger *OperatorApplyLedger, objects *OperatorPhysicalObjects, compute *OperatorProcessCompute, request OperatorInteractiveMask) (OperatorMeasuredTask, review.MaskRef, error) {
	var empty OperatorMeasuredTask
	var mask review.MaskRef
	fail := func() (OperatorMeasuredTask, review.MaskRef, error) {
		return empty, mask, errors.New("operator interactive mask binding invalid")
	}
	if ledger == nil || objects == nil || compute == nil || compute.Ledger != ledger || compute.Objects != objects || request.Task == uuid.Nil || request.Item == uuid.Nil || (request.Backend != "cpu" && request.Backend != "mps") || (request.Kind != "mask_manual" && request.Kind != "mask_prompt") {
		return fail()
	}
	if _, exists := ledger.tasks[request.Task]; exists {
		return fail()
	}
	var source *applyInput
	for i := range ledger.inputs {
		if ledger.inputs[i].ItemID == request.Item {
			source = &ledger.inputs[i]
			break
		}
	}
	if source == nil {
		return fail()
	}
	raw, err := canonical.Bytes(request.Params)
	if err != nil || !bytes.Equal(raw, request.Params) || len(raw) < 2 || len(raw) > 1<<20 {
		return fail()
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return empty, mask, err
	}
	op := "mask.prompt"
	role := "prompt_mask_ref"
	if request.Kind == "mask_manual" {
		if request.Backend != "cpu" {
			return fail()
		}
		op = "mask.rasterize"
		role = "mask_target_ref"
	}
	declared, err := canonical.Marshal(request.Declared)
	if err != nil {
		return empty, mask, err
	}
	lease := InteractiveLease{TenantID: ledger.lease.TenantID, RunID: ledger.lease.RunID, TaskID: request.Task, SourceID: source.Image.ArtifactID, Kind: request.Kind, Operation: op, Params: raw, ParamsSHA: sha, Declared: declared, Backend: request.Backend, RuntimeLock: request.RuntimeLock}
	if request.Kind == "mask_manual" {
		planned, e := verifyPlannedInteractiveManual(raw, sha, request.Task, source.Image.ref(), source.Working)
		if e != nil {
			return empty, mask, e
		}
		if !planned {
			return fail()
		}
	}
	// Pure validation identity is never used as an accepted document receipt.
	validation := artifactjson.Ref{ID: uuid.NewSHA1(request.Task, []byte("operator-interactive-compile")), SHA256: sha}
	materialized, err := CompileInteractiveMaskMaterialization(lease, source.Image, source.Working, validation)
	if err != nil {
		return empty, mask, err
	}
	node := applyNodePlan{ID: request.Task, Phase: "interactive_prerequisite", Ordinal: source.Ordinal, SourceItemID: source.ItemID, SourceAssetID: source.AssetID, SourceVersionID: source.VersionID, NodeKey: request.Kind + "/" + request.Task.String(), Operation: op, Inputs: materialized.Inputs, ParamsSHA: sha, Params: raw}
	ledger.plans[node.ID] = node
	params, err := (OperatorDocumentPublisher{Objects: objects, Ledger: ledger}).Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID, Run: lease.RunID, Owner: node.ID, Purpose: "task_params"}, raw)
	if err != nil {
		return empty, mask, err
	}
	materialized, err = CompileInteractiveMaskMaterialization(lease, source.Image, source.Working, params)
	if err != nil {
		return empty, mask, err
	}
	reg, err := LoadRegistry()
	if err != nil {
		return empty, mask, err
	}
	if ledger.mixedChildren != nil {
		materialized.Profile, err = ledger.mixedChildren.QualifyProfile(lease.TenantID, lease.RunID, ledger.mixedCorpus, op, materialized.Profile)
		if err != nil {
			return empty, mask, err
		}
	}
	task, err := prepareApplyMaterialization(reg, node, review.ArtifactRef{ID: params.ID, SHA256: params.SHA256}, materialized.Profile)
	if err != nil {
		return empty, mask, err
	}
	ledger.tasks[task.ID] = task
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSeconds)*time.Second)
	measured, executeErr := executeMeasuredOperator(taskCtx, task, compute.Execute)
	deadlineErr := taskCtx.Err()
	cancel()
	if executeErr != nil {
		return empty, mask, executeErr
	}
	if deadlineErr != nil {
		return empty, mask, deadlineErr
	}
	if err = ledger.AcceptMeasuredTask(ctx, task, measured); err != nil {
		return empty, mask, err
	}
	var root *styleArtifact
	for _, out := range measured.Outputs {
		if out.Role == role {
			if root != nil {
				return fail()
			}
			copy := out.Artifact
			root = &copy
		}
	}
	if root == nil {
		return fail()
	}
	body, err := readApplyJSON(ctx, objects, "operator-measurement", *root)
	if err != nil {
		return empty, mask, err
	}
	mask, err = review.ParseMaskRef(body)
	if err != nil {
		return empty, mask, err
	}
	if mask.Canvas != source.Working.Canvas || !bytes.Equal(mask.TransformChain, source.Working.TransformChain) {
		return fail()
	}
	retained, exists := ledger.objects[mask.Artifact.ID]
	if !exists || retained.SHA256 != mask.Artifact.SHA256 {
		return fail()
	}
	return measured, mask, nil
}
