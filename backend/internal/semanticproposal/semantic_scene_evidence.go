package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func semanticSceneSourceFromResults(ctx context.Context, scenePlan, depthPlan semanticSceneStagePlan, scene, depth semanticSceneStageResult) (v2input.SemanticSceneSource, error) {
	var zero v2input.SemanticSceneSource
	if err := verifySemanticSceneStagePlan(ctx, scenePlan); err != nil {
		return zero, err
	}
	if err := verifySemanticSceneStagePlan(ctx, depthPlan); err != nil {
		return zero, err
	}
	a, b := scenePlan.prepared.published.anchor, depthPlan.prepared.published.anchor
	if a.operation != "scene.parse" || b.operation != "depth.predict" || a.source != b.source || a.tenant != b.tenant || a.project != b.project || a.actor != b.actor || a.session != b.session || a.parent != b.parent || a.requestSHA != b.requestSHA || scene.planSeal != scenePlan.seal || depth.planSeal != depthPlan.seal {
		return zero, v2input.ErrConflict
	}
	for _, item := range []struct {
		plan   semanticSceneStagePlan
		result semanticSceneStageResult
	}{{scenePlan, scene}, {depthPlan, depth}} {
		if item.result.value.Run != item.plan.shell.Run || item.result.value.Task != item.plan.shell.Tasks[0].ID || item.result.value.ConfigSHA != item.plan.shell.ConfigSHA || item.result.value.Params != item.plan.prepared.published.ref || item.result.catalog == "" {
			return zero, v2input.ErrConflict
		}
		if err := validateSemanticSceneResultDocument(item.plan, item.result.value, json.RawMessage(item.result.rootBody)); err != nil {
			return zero, err
		}
	}
	return v2input.SemanticSceneSource{Binding: a.source, Labels: scene.value.Root, LabelsParams: scene.value.Params, LabelsPixels: scene.value.Pixels, Depth: depth.value.Root, DepthParams: depth.value.Params, DepthPixels: depth.value.Pixels, DepthInvalid: depth.value.Invalid}, ctx.Err()
}

// Only producer-qualified immutable results can reach this server compiler.
// Scientific plane/flow safety is still independently measured by worker and
// adopter; this function does not claim that metadata proves planarity.
func compileSemanticSceneEvidence(ctx context.Context, request Request, intent v2input.Ref, pin v2input.SemanticQualificationPin, policy semanticScenePolicyProof, reference, target v2input.SemanticSceneSource) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := policy.qualification
	if q.tenant != request.TenantID || q.actor != request.ActorID || q.pin != pin || reference.Binding != request.Intent.Reference {
		return nil, v2input.ErrConflict
	}
	value, sha, err := v2input.DecodeSemanticScenePolicy([]byte(policy.policy))
	if err != nil || sha != policy.sha {
		return nil, v2input.ErrInvalid
	}
	evidence := v2input.SemanticSceneEvidence{SchemaVersion: 1, Reference: reference, Target: target, Intent: intent, Calibration: pin.Calibration, Policy: value, PolicySHA: sha, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA}
	body, err := canonical.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	if _, err := v2input.DecodeSemanticSceneEvidence(body); err != nil {
		return nil, err
	}
	return body, ctx.Err()
}

// Costs are added once to the same base estimator used by normal transfer. The
// complete embedded document remains in the immutable execution profile and its
// typed physical refs are separately registered by SemanticSceneEntries.
func semanticSceneTransferProfile(reference, target v2input.SessionVersionBinding, referenceBody, targetBody json.RawMessage, evidence json.RawMessage, c TransferPreparation) (json.RawMessage, v2input.SemanticQualifiedCosts, error) {
	var zero v2input.SemanticQualifiedCosts
	scene, err := v2input.DecodeSemanticSceneEvidence(evidence)
	if err != nil || scene.Reference.Binding != reference || scene.Target.Binding != target || scene.ModelLockSHA != c.Qualification.ModelLockSHA || scene.RuntimeLockSHA != c.Qualification.RuntimeLockSHA || scene.DeviceProfileSHA != c.Qualification.DeviceProfileSHA || scene.DatasetSHA != c.Qualification.DatasetSHA {
		return nil, zero, v2input.ErrInvalid
	}
	a, err := v2input.ResolveSemanticSourceMetadata(reference, referenceBody)
	if err != nil {
		return nil, zero, err
	}
	b, err := v2input.ResolveSemanticSourceMetadata(target, targetBody)
	if err != nil {
		return nil, zero, err
	}
	costs, err := semanticSceneCosts(a.Width, a.Height, b.Width, b.Height, scene.Policy.ProxyEdge, c.Costs)
	if err != nil {
		return nil, zero, err
	}
	profile, err := v2input.CompileSemanticTransferProfileForBackend(c.Expected.Backend, a.Width, a.Height, b.Width, b.Height, a.Gamma, b.Gamma, costs, c.RAM, c.VRAM, c.Scratch, c.Output)
	if err != nil {
		return nil, zero, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil {
		return nil, zero, v2input.ErrInvalid
	}
	fields["semantic_scene"] = append(json.RawMessage(nil), evidence...)
	frozen, err := canonical.Marshal(fields)
	return frozen, costs, err
}
