package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLTransferSourceReader) prepareNativeTargetFaceTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, plan nativeTargetDetectorPlan, color TransferColorProfile, published nativeFaceStageParams, report v2input.Ref, budget nativePreparationBudget, profileVersions ...int) (nativeFaceTaskPreparation, error) {
	var zero nativeFaceTaskPreparation
	version, err := nativeModelProfileVersion(profileVersions)
	if err != nil {
		return zero, fmt.Errorf("target face model profile version: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("target face context: %w", err)
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, fmt.Errorf("target face detector plan: %w", err)
	}
	_, workflow, err := nativeFacePreparationIDs(plan.anchor.entry.Namespace, published.operation)
	if err != nil {
		return zero, fmt.Errorf("target face workflow identity: %w", err)
	}
	if published.document != uuid.NewSHA1(plan.anchor.entry.Namespace, []byte("semantic."+published.operation+".params:reference")) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(plan.detector.policy, []string{published.operation})
	if err != nil {
		return zero, fmt.Errorf("target face qualification policy: %w", err)
	}
	expected := selected[published.operation]
	if expected.Backend != "cuda" && expected.Backend != "mps" {
		return zero, v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(budget.Costs, v2qualification.Metrics{}); err != nil {
		return zero, fmt.Errorf("target face qualified costs: %w", err)
	}
	before, err := s.readCompletedNativeTargetDetector(ctx, who, plan, color)
	if err != nil {
		return zero, fmt.Errorf("target face completed detector: %w", err)
	}
	if published.detector != before.catalog || published.sourceBody != before.sourceBody || published.facesBody != before.body || published.detectorParamsBody != before.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	image, faces, detector := json.RawMessage(before.sourceBody), json.RawMessage(before.body), json.RawMessage(before.detectorParamsBody)
	binding := plan.anchor.entry.Binding
	var paramsBody json.RawMessage
	var entries []v2input.Entry
	if published.operation == "face.landmarks" {
		compiled, err := v2input.PrepareSemanticLandmarkParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, expected.ModelLockSHA)
		if err != nil {
			return zero, fmt.Errorf("target face landmark params: %w", err)
		}
		paramsBody, entries = compiled.Params, compiled.Inputs
	} else {
		compiled, err := v2input.PrepareSemanticFaceParseParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, expected.ModelLockSHA)
		if err != nil {
			return zero, fmt.Errorf("target face parser params: %w", err)
		}
		paramsBody, entries = compiled.Params, compiled.Inputs
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return zero, fmt.Errorf("target face source metadata: %w", err)
	}
	nodes := []v2draft.LayerRef{{Ref: published.ref, Contract: "Params"}}
	sizes := []v2input.SemanticInputSize{{Ref: published.ref, Bytes: int64(len(paramsBody))}}
	for _, entry := range entries {
		node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract}
		switch {
		case entry.Ref == binding.Source:
			node.CanvasHash = binding.CanvasHash
		case entry.Ref == before.catalog.Output:
			node.ParentRef = binding.Source
		case entry.Ref == metadata.Inputs.Pixels:
			node.ParentRef = binding.Source
			node.Contract = "WorkingPixels"
		case metadata.Inputs.Alpha != nil && entry.Ref == *metadata.Inputs.Alpha:
			node.ParentRef = binding.Source
			node.Contract = "WorkingAlpha"
		default:
			node.ParentRef = binding.Source
			node.Contract = "DenseWarpField"
		}
		nodes = append(nodes, node)
		sizes = append(sizes, v2input.SemanticInputSize{Ref: entry.Ref, Bytes: 1})
	}
	compile := func(sizes []v2input.SemanticInputSize) (nativeRegistrationTask, error) {
		if published.operation == "face.landmarks" {
			return v2input.CompileSemanticLandmarkTaskForBackend(expected.Backend, ctx, workflow, 0, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, published.ref, expected.ModelLockSHA, sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
		}
		return v2input.CompileSemanticFaceParseTaskForBackend(expected.Backend, ctx, workflow, 0, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, published.ref, expected.ModelLockSHA, sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	}
	if _, err := compile(sizes); err != nil {
		return zero, fmt.Errorf("target face initial task capacity: %w", err)
	}
	originalReport, err := s.faceQualificationObject(ctx, who, plan.detector.request.TenantID, expected, report)
	if err != nil {
		return zero, fmt.Errorf("target face qualification object: %w", err)
	}
	rawReport, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, originalReport.object, originalReport.ref)
	if err != nil {
		return zero, fmt.Errorf("target face qualification bytes: %w", err)
	}
	verified, err := v2qualification.VerifyTyped(rawReport, report.SHA256, expected)
	if err != nil {
		return zero, fmt.Errorf("target face qualification verification: %w", err)
	}
	if err := validateTransferResourceCosts(budget.Costs, verified.Resources()); err != nil {
		return zero, fmt.Errorf("target face measured costs: %w", err)
	}
	scoped := nativeFaceInputCatalog{params: detectorParamsCatalog{reader: s, source: catalog, binding: binding, document: published.document, params: published.ref}, detector: before.catalog}
	evidence, err := v2draft.ReadInputEvidence(ctx, scoped, who, plan.detector.request.TenantID, nodes)
	if err != nil {
		return zero, fmt.Errorf("target face input evidence: %w", err)
	}
	if err := verifyNativeFaceSourceEvidence(ctx, plan.detector.prepared.source, before.catalog.Output, published.ref, evidence); err != nil {
		return zero, fmt.Errorf("target face source evidence: %w", err)
	}
	sizes = make([]v2input.SemanticInputSize, len(evidence))
	for i, entry := range evidence {
		sizes[i] = v2input.SemanticInputSize{Ref: entry.Ref, Bytes: entry.Object.Bytes}
		if (entry.Ref == published.ref && entry.Object.Bytes != int64(len(paramsBody))) || (entry.Ref == binding.Source && entry.Object.Bytes != int64(len(image))) || (entry.Ref == before.catalog.Output && entry.Object.Bytes != int64(len(faces))) {
			return zero, v2input.ErrInvalid
		}
	}
	frozen, err := compile(sizes)
	if err != nil {
		return zero, fmt.Errorf("target face frozen task: %w", err)
	}
	if version == 1 {
		frozen, err = bindNativeFaceExecutionIdentity(frozen, expected)
		if err != nil {
			return zero, fmt.Errorf("target face execution identity: %w", err)
		}
	}
	pins, err := canonical.Marshal(evidence)
	if err != nil || len(pins) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return zero, fmt.Errorf("target face draft task: %w", err)
	}
	if err := validateNativeModelProfileCapacity(task, budget.RAM, budget.VRAM, budget.Scratch, budget.Output); err != nil {
		return zero, fmt.Errorf("target face capacity verification: %w", err)
	}
	taskSHA, err := canonical.Hash(task)
	if err != nil {
		return zero, fmt.Errorf("target face task digest: %w", err)
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return zero, fmt.Errorf("target face qualification encoding: %w", err)
	}
	currentEvidence, err := v2draft.ReadInputEvidence(ctx, scoped, who, plan.detector.request.TenantID, nodes)
	if err != nil {
		return zero, fmt.Errorf("target face repeated input evidence: %w", err)
	}
	currentPins, err := canonical.Marshal(currentEvidence)
	if err != nil || string(currentPins) != string(pins) {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeTargetDetector(ctx, who, plan, color)
	if err != nil {
		return zero, fmt.Errorf("target face repeated evidence encoding: %w", err)
	}
	if after.catalog != before.catalog || after.body != before.body || after.sourceBody != before.sourceBody || after.detectorParamsBody != before.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	currentReport, err := s.faceQualificationObject(ctx, who, plan.detector.request.TenantID, expected, report)
	if err != nil {
		return zero, fmt.Errorf("target face repeated detector: %w", err)
	}
	if currentReport != originalReport {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("target face repeated qualification: %w", err)
	}
	policySHA, err := canonical.Hash(json.RawMessage(plan.detector.policy.body))
	if err != nil {
		return zero, fmt.Errorf("target face final context: %w", err)
	}
	return nativeFaceTaskPreparation{costs: budget.Costs, operation: published.operation, document: published.document, params: published.ref, detector: before.catalog, frozen: frozen, inputs: string(pins), expected: string(expectedBody), taskSHA: taskSHA, report: originalReport, metrics: verified.Resources(), tenant: plan.detector.request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: plan.anchor.entry.Namespace, bucket: s.Bucket, policySHA: policySHA}, nil
}
