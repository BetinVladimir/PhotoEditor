package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeFaceInputCatalog struct {
	params   detectorParamsCatalog
	detector nativeDetectorResultCatalog
}

func (c nativeFaceInputCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if node.Contract != "FaceBoxes" {
		return c.params.Lookup(ctx, who, tenant, node)
	}
	if node.Ref != c.detector.Output || node.ParentRef != c.params.binding.Source || node.CanvasHash != "" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	objects, err := c.params.reader.facePreparationObjects(ctx, who, tenant, c.params.binding, c.detector.Output, "face.detect")
	if err != nil {
		return objectstore.Object{}, err
	}
	if objects[1].ref != c.detector.Params {
		return objectstore.Object{}, v2input.ErrConflict
	}
	return objects[0].object, nil
}

type nativeFaceTaskPreparation struct {
	legacyQueueOmitted                      bool
	costs                                   v2input.SemanticQualifiedCosts
	operation                               string
	document                                uuid.UUID
	params                                  v2input.Ref
	detector                                nativeDetectorResultCatalog
	frozen                                  nativeRegistrationTask
	inputs, expected, taskSHA               string
	report                                  personEvidenceObject
	metrics                                 v2qualification.ResourceEvidence
	tenant, project, actor, session, parent uuid.UUID
	bucket, policySHA                       string
}

func nativeFacePreparationIDs(parent uuid.UUID, operation string) (uuid.UUID, uuid.UUID, error) {
	if parent == uuid.Nil || (operation != "face.landmarks" && operation != "face.parse") {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(parent, []byte("semantic."+operation+":reference"))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

// Compiles an owned native task from accepted-version Params and the exact
// completed SFD. The original imported downstream report is independently
// verified. Registration, final authority locks and queueing are separate.
func (s SQLTransferSourceReader) prepareNativeFaceTask(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile, published nativeFaceStageParams, report v2input.Ref, budget nativePreparationBudget, profileVersions ...int) (nativeFaceTaskPreparation, error) {
	var zero nativeFaceTaskPreparation
	version, err := nativeModelProfileVersion(profileVersions)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	_, workflow, err := nativeFacePreparationIDs(plan.request.RunID, published.operation)
	if err != nil {
		return zero, err
	}
	if published.document != uuid.NewSHA1(plan.request.RunID, []byte("semantic."+published.operation+".params:reference")) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(plan.policy, []string{published.operation})
	if err != nil {
		return zero, err
	}
	expected := selected[published.operation]
	if expected.Backend != "cuda" && expected.Backend != "mps" {
		return zero, v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(budget.Costs, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	before, err := s.readCompletedDetectorPreparation(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if published.detector != before.catalog || published.sourceBody != before.sourceBody || published.facesBody != before.body || published.detectorParamsBody != before.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	image, faces, detector := json.RawMessage(before.sourceBody), json.RawMessage(before.body), json.RawMessage(before.detectorParamsBody)
	binding := plan.request.Intent.Reference
	var paramsBody json.RawMessage
	var entries []v2input.Entry
	if published.operation == "face.landmarks" {
		compiled, err := v2input.PrepareSemanticLandmarkParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, expected.ModelLockSHA)
		if err != nil {
			return zero, err
		}
		paramsBody, entries = compiled.Params, compiled.Inputs
	} else {
		compiled, err := v2input.PrepareSemanticFaceParseParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, expected.ModelLockSHA)
		if err != nil {
			return zero, err
		}
		paramsBody, entries = compiled.Params, compiled.Inputs
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return zero, err
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
		return zero, err
	}
	originalReport, err := s.faceQualificationObject(ctx, who, plan.request.TenantID, expected, report)
	if err != nil {
		return zero, err
	}
	rawReport, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, originalReport.object, originalReport.ref)
	if err != nil {
		return zero, err
	}
	verified, err := v2qualification.VerifyTyped(rawReport, report.SHA256, expected)
	if err != nil {
		return zero, err
	}
	if err := validateTransferResourceCosts(budget.Costs, verified.Resources()); err != nil {
		return zero, err
	}
	scoped := nativeFaceInputCatalog{params: detectorParamsCatalog{reader: s, source: catalog, binding: binding, document: published.document, params: published.ref}, detector: before.catalog}
	evidence, err := v2draft.ReadInputEvidence(ctx, scoped, who, plan.request.TenantID, nodes)
	if err != nil {
		return zero, err
	}
	if err := verifyNativeFaceSourceEvidence(ctx, plan.prepared.source, before.catalog.Output, published.ref, evidence); err != nil {
		return zero, err
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
		return zero, err
	}
	if version == 1 {
		frozen, err = bindNativeFaceExecutionIdentity(frozen, expected)
		if err != nil {
			return zero, err
		}
	}
	pins, err := canonical.Marshal(evidence)
	if err != nil || len(pins) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if err := validateNativeModelProfileCapacity(task, budget.RAM, budget.VRAM, budget.Scratch, budget.Output); err != nil {
		return zero, err
	}
	taskSHA, err := canonical.Hash(task)
	if err != nil {
		return zero, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	currentEvidence, err := v2draft.ReadInputEvidence(ctx, scoped, who, plan.request.TenantID, nodes)
	if err != nil {
		return zero, err
	}
	currentPins, err := canonical.Marshal(currentEvidence)
	if err != nil || string(currentPins) != string(pins) {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedDetectorPreparation(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if after.catalog != before.catalog || after.body != before.body || after.sourceBody != before.sourceBody || after.detectorParamsBody != before.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	currentReport, err := s.faceQualificationObject(ctx, who, plan.request.TenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if currentReport != originalReport {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	policySHA, err := canonical.Hash(json.RawMessage(plan.policy.body))
	if err != nil {
		return zero, err
	}
	return nativeFaceTaskPreparation{costs: budget.Costs, operation: published.operation, document: published.document, params: published.ref, detector: before.catalog, frozen: frozen, inputs: string(pins), expected: string(expectedBody), taskSHA: taskSHA, report: originalReport, metrics: verified.Resources(), tenant: plan.request.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, parent: plan.request.RunID, bucket: s.Bucket, policySHA: policySHA}, nil
}

// A supplied source catalog cannot substitute metadata used for GPU accounting.
// Join every original source object to the byte-verified retained source proof.
func verifyNativeFaceSourceEvidence(ctx context.Context, proof sourcePreparationProof, faces, params v2input.Ref, evidence []v2draft.InputEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var pins []sourcePreparationPin
	if len(proof.pins) > 4<<20 || json.Unmarshal([]byte(proof.pins), &pins) != nil || len(pins) < 1 || len(pins) > 4095 || len(evidence) != len(pins)+2 {
		return v2input.ErrInvalid
	}
	expected := make(map[uuid.UUID]sourcePreparationPin, len(pins))
	for _, pin := range pins {
		if pin.Node.Ref.ArtifactID == uuid.Nil || pin.Node.Ref.ArtifactID == faces.ArtifactID || pin.Node.Ref.ArtifactID == params.ArtifactID {
			return v2input.ErrInvalid
		}
		if _, exists := expected[pin.Node.Ref.ArtifactID]; exists {
			return v2input.ErrInvalid
		}
		expected[pin.Node.Ref.ArtifactID] = pin
	}
	seen := make(map[uuid.UUID]bool, len(evidence))
	for _, entry := range evidence {
		if seen[entry.Ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
		if pin, ok := expected[entry.Ref.ArtifactID]; ok {
			if entry.Ref != pin.Node.Ref || entry.Contract != pin.Node.Contract || entry.Object != pin.Object {
				return v2input.ErrConflict
			}
		} else if entry.Ref == faces {
			if entry.Contract != "FaceBoxes" {
				return v2input.ErrInvalid
			}
		} else if entry.Ref == params {
			if entry.Contract != "Params" {
				return v2input.ErrInvalid
			}
		} else {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}
