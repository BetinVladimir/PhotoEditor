package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"time"
)

// OperatorSemanticPrerequisite executes the existing semantic source domain,
// independently of Apply. Resources are externally approved operator limits,
// not a fabricated SemanticQualifiedCosts report or a qualification callback.
type OperatorSemanticPrerequisite struct {
	Item      uuid.UUID                  `json:"item_id"`
	ModelLock string                     `json:"model_lock_sha"`
	Profiles  map[string]json.RawMessage `json:"profiles"`
}

func MeasureSemanticPrerequisites(ctx context.Context, s *OperatorApplyLedger, objects *OperatorPhysicalObjects, compute *OperatorProcessCompute, request OperatorSemanticPrerequisite) ([]OperatorMeasuredTask, error) {
	if s == nil || objects == nil || compute == nil || compute.Ledger != s || compute.Objects != objects || len(request.Profiles) != 3 {
		return nil, errors.New("operator semantic source scope invalid")
	}
	var source *applyInput
	for i := range s.inputs {
		if s.inputs[i].ItemID == request.Item {
			source = &s.inputs[i]
			break
		}
	}
	if source == nil {
		return nil, errors.New("operator semantic item missing")
	}
	imageBody, err := readApplyJSON(ctx, objects, "operator-measurement", source.Image)
	if err != nil {
		return nil, err
	}
	canvas, err := canonical.Hash(source.Working.Canvas)
	if err != nil {
		return nil, err
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: source.VersionID, Source: v2input.Ref{ArtifactID: source.Image.ArtifactID, SHA256: source.Image.SHA256}, CanvasHash: canvas}
	scene, err := v2input.PrepareSemanticAnalysisParams(ctx, "scene.parse", binding, imageBody, request.ModelLock)
	if err != nil {
		return nil, err
	}
	detector, err := v2input.PrepareSemanticFaceDetectParams(ctx, binding, imageBody, request.ModelLock, .8, 64)
	if err != nil {
		return nil, err
	}
	measured := []OperatorMeasuredTask{}
	run := func(op string, body json.RawMessage, entries []v2input.Entry) (OperatorMeasuredTask, error) {
		var zero OperatorMeasuredTask
		profile, ok := request.Profiles[op]
		if !ok {
			return zero, errors.New("operator semantic profile missing")
		}
		if s.mixedChildren != nil {
			var bindErr error
			profile, bindErr = s.mixedChildren.QualifyProfile(s.lease.TenantID, s.lease.RunID, s.mixedCorpus, op, profile)
			if bindErr != nil {
				return zero, bindErr
			}
		}
		var limits struct {
			Backend  string           `json:"backend"`
			Estimate ResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(profile, &limits) != nil || (limits.Backend != "cpu" && limits.Backend != "mps") || limits.Estimate.RAMBytes < 1 || limits.Estimate.ScratchBytes < 1 || limits.Estimate.OutputBytes < 1 || ((limits.Backend == "cpu" && limits.Estimate.VRAMBytes != 0) || (limits.Backend == "mps" && limits.Estimate.VRAMBytes < 1)) {
			return zero, errors.New("operator semantic CPU resource bounds invalid")
		}
		// A profile may not pretend an already-qualified deployment during first
		// qualification. The installed model/device bytes are separately pinned by
		// the operator compute policy and actual ModelPool.
		var keys map[string]json.RawMessage
		if json.Unmarshal(profile, &keys) != nil {
			return zero, errors.New("operator profile invalid")
		}
		for _, key := range []string{"model_lock_sha", "runtime_lock_sha", "device_profile_sha"} {
			if _, exists := keys[key]; exists {
				return zero, errors.New("operator qualification authority forbidden")
			}
		}
		id := uuid.NewSHA1(s.lease.RunID, []byte("operator/semantic/"+source.ItemID.String()+"/"+op))
		if _, exists := s.tasks[id]; exists {
			return zero, errors.New("operator semantic task replay")
		}
		inputs := make([]review.ArtifactRef, len(entries))
		for i, e := range entries {
			inputs[i] = review.ArtifactRef{ID: e.Ref.ArtifactID, SHA256: e.Ref.SHA256}
		}
		pin, e := canonical.Hash(body)
		if e != nil {
			return zero, e
		}
		node := applyNodePlan{ID: id, Phase: "semantic_prerequisite", Ordinal: source.Ordinal, SourceItemID: source.ItemID, SourceAssetID: source.AssetID, SourceVersionID: source.VersionID, NodeKey: "operator/semantic/" + source.ItemID.String() + "/" + op, Operation: op, Params: body, ParamsSHA: pin, Inputs: inputs}
		s.plans[id] = node
		ref, e := (OperatorDocumentPublisher{Objects: objects, Ledger: s}).Publish(ctx, artifactjson.Scope{Tenant: s.lease.TenantID, Run: s.lease.RunID, Owner: id, Purpose: "task_params"}, body)
		if e != nil {
			return zero, e
		}
		registry, e := LoadRegistry()
		if e != nil {
			return zero, e
		}
		task, e := prepareApplyMaterialization(registry, node, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}, profile)
		if e != nil {
			return zero, e
		}
		s.tasks[id] = task
		taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSeconds)*time.Second)
		observation, executeErr := executeMeasuredOperator(taskCtx, task, compute.Execute)
		deadlineErr := taskCtx.Err()
		cancel()
		if executeErr != nil {
			return zero, executeErr
		}
		if deadlineErr != nil {
			return zero, deadlineErr
		}
		if e = s.AcceptMeasuredTask(ctx, task, observation); e != nil {
			return zero, e
		}
		measured = append(measured, observation)
		return observation, nil
	}
	if _, err = run("scene.parse", scene.Params, scene.Inputs); err != nil {
		return nil, err
	}
	boxes, err := run("face.detect", detector.Params, detector.Inputs)
	if err != nil {
		return nil, err
	}
	var faces *styleArtifact
	for _, out := range boxes.Outputs {
		if out.Role == "face_boxes" {
			if faces != nil {
				return nil, errors.New("duplicate face boxes")
			}
			value := out.Artifact
			faces = &value
		}
	}
	if faces == nil {
		return nil, errors.New("actual detector boxes missing")
	}
	facesBody, err := readApplyJSON(ctx, objects, "operator-measurement", *faces)
	if err != nil {
		return nil, err
	}
	detectorTask := s.tasks[boxes.Task.ID]
	parser, err := v2input.PrepareSemanticFaceParseParams(ctx, binding, imageBody, v2input.Ref{ArtifactID: faces.ArtifactID, SHA256: faces.SHA256}, facesBody, v2input.Ref{ArtifactID: detectorTask.Params.ID, SHA256: detectorTask.Params.SHA256}, detector.Params, request.ModelLock)
	if err != nil {
		return nil, err
	}
	if _, err = run("face.parse", parser.Params, parser.Inputs); err != nil {
		return nil, err
	}
	return measured, nil
}
