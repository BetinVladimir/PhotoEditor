package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

type nativeAllTargetTransferTaskInput struct {
	reference nativeInputTransferReferenceDocuments
	target    nativeTransferTargetProof
	qualified transferQualificationPreparationProof
	profile   string
	scene     json.RawMessage
}

// This type deliberately has no generic personProtectionProof. Native target
// authority must be locked by the dedicated admission path, never cast/imported.
type nativeAllTargetTransferWorkflowPlan struct {
	core      WorkflowPlan
	reference nativeInputTransferReferenceDocuments
	targets   []nativeTransferTargetProof
}

func nativeAllTargetTransferProfile(request Request, reference nativeInputTransferReferenceDocuments, target nativeTransferTargetProof, c TransferPreparation) (json.RawMessage, error) {
	source, err := v2input.ResolveSemanticSourceMetadata(request.Intent.Reference, reference.reference)
	if err != nil {
		return nil, err
	}
	destination, err := v2input.ResolveSemanticSourceMetadata(target.binding, []byte(target.image))
	if err != nil {
		return nil, err
	}
	if len(c.sceneEvidence) > 0 {
		if target.ordinal < 0 || target.ordinal >= len(c.sceneEvidence) {
			return nil, v2input.ErrInvalid
		}
		profile, _, err := semanticSceneTransferProfile(request.Intent.Reference, target.binding, reference.reference, json.RawMessage(target.image), c.sceneEvidence[target.ordinal], c)
		return profile, err
	}
	return v2input.CompileSemanticTransferProfileForBackend(c.Expected.Backend, source.Width, source.Height, destination.Width, destination.Height, source.Gamma, destination.Gamma, c.Costs, c.RAM, c.VRAM, c.Scratch, c.Output)
}
func (s SQLTransferSourceReader) prepareNativeAllTargetTransferInputs(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, c TransferPreparation, reference nativeInputTransferReferenceDocuments, targets []nativeTransferTargetProof) ([]nativeAllTargetTransferTaskInput, []v2input.SemanticPinnedDocuments, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	c = ownTransferPreparation(c)
	if err := validateTransferConfiguration(c); err != nil {
		return nil, nil, err
	}
	if !reference.person.originalPinned {
		return nil, nil, v2input.ErrInvalid
	}
	if err := s.validateNativeInputTransferReference(ctx, who, request, c, reference); err != nil {
		return nil, nil, err
	}
	n := len(targets)
	if len(c.sceneEvidence) > 0 && len(c.sceneEvidence) != n {
		return nil, nil, v2input.ErrInvalid
	}
	if n < 1 || n > 5000 || n != len(c.TargetProtection) || n != len(published.Inventory.Targets) || n != published.Inventory.Scope.ItemCount || published.Inventory.Scope.ProjectID != s.ProjectID {
		return nil, nil, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, nil, err
	}
	if !sameTransferInventory(before, published.Inventory) {
		return nil, nil, v2input.ErrConflict
	}
	q, err := s.readTransferQualificationPreparation(ctx, who, request.TenantID, c.Qualification, c.Expected)
	if err != nil {
		return nil, nil, err
	}
	if err := validateTransferMeasuredCosts(c.Costs, q.metrics); err != nil {
		return nil, nil, err
	}
	var retained int64
	if err := chargeMetadata(&retained, c.MetadataBytes, n*1024, len(q.expected)); err != nil {
		return nil, nil, err
	}
	inputs := make([]nativeAllTargetTransferTaskInput, n)
	documents := make([]v2input.SemanticPinnedDocuments, n)
	for i, t := range targets {
		if t.ordinal != i || t.binding != before.Targets[i] || t.protection.Root != c.TargetProtection[i] {
			return nil, nil, v2input.ErrConflict
		}
		if err := s.verifyNativeTransferTarget(ctx, who, request, reference, t); err != nil {
			return nil, nil, err
		}
		profile, err := nativeAllTargetTransferProfile(request, reference, t, c)
		if err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, c.MetadataBytes, len(reference.reference), len(reference.selected), len(reference.protection), len(t.completed), len(t.image), len(t.protection.Body), len(profile), len(t.source.pins)); err != nil {
			return nil, nil, err
		}
		inputs[i] = nativeAllTargetTransferTaskInput{reference: reference, target: t, qualified: q, profile: string(profile)}
		if len(c.sceneEvidence) > 0 {
			inputs[i].scene = append(json.RawMessage(nil), c.sceneEvidence[i]...)
		}
		documents[i] = v2input.SemanticPinnedDocuments{Reference: append(json.RawMessage(nil), reference.reference...), SelectedMask: append(json.RawMessage(nil), reference.selected...), SourceProtection: append(json.RawMessage(nil), reference.protection...), Target: json.RawMessage(t.image), TargetProtection: append(json.RawMessage(nil), t.protection.Body...)}
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, nil, err
	}
	if !sameTransferInventory(before, after) {
		return nil, nil, v2input.ErrConflict
	}
	current, err := s.qualificationObjects(ctx, who, request.TenantID, c.Qualification)
	if err != nil {
		return nil, nil, err
	}
	if len(current) != 2 || current[0] != q.objects[0] || current[1] != q.objects[1] {
		return nil, nil, v2input.ErrConflict
	}
	for _, input := range inputs {
		t := input.target
		if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, t.binding, c.Color, t.source.unitRGB, t.source); err != nil {
			return nil, nil, err
		}
		if t.plan != nil {
			object, err := s.nativeTargetDefectProtectionObject(ctx, who, *t.plan)
			if err != nil {
				return nil, nil, err
			}
			if object != t.registered.object {
				return nil, nil, v2input.ErrConflict
			}
			result, err := s.nativeTargetDefectResultCatalog(ctx, who, *t.plan)
			if err != nil {
				return nil, nil, err
			}
			if !reflect.DeepEqual(result, t.registered.published.preparation.completed.catalog) {
				return nil, nil, v2input.ErrConflict
			}
		}
	}
	checked, err := s.readNativeInputTransferReferenceDocuments(ctx, who, request, reference.person, c.Color, c.MetadataBytes)
	if err != nil {
		return nil, nil, err
	}
	if !sameNativeInputTransferDocuments(ctx, checked, reference) {
		return nil, nil, v2input.ErrConflict
	}
	return inputs, documents, ctx.Err()
}

// Pure task freezing after physical preparation. Current SQL authority is still
// mandatory before Begin/register/seal; this compiler returns no public ticket.
func (s SQLTransferSourceReader) compileNativeAllTargetTransferWorkflowPlan(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, params []v2input.Ref, inputs []nativeAllTargetTransferTaskInput, documents []v2input.SemanticPinnedDocuments, c TransferPreparation) (nativeAllTargetTransferWorkflowPlan, error) {
	var zero nativeAllTargetTransferWorkflowPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	c = ownTransferPreparation(c)
	if err := validateTransferConfiguration(c); err != nil {
		return zero, err
	}
	n := len(inputs)
	if who.UserID != request.ActorID || who.SessionID == uuid.Nil || n < 1 || n > 5000 || n != len(params) || n != len(documents) || n != len(c.TargetProtection) {
		return zero, v2input.ErrInvalid
	}
	targets, err := BindTargets(request, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if len(targets) != n || len(published.Inventory.Targets) != n {
		return zero, v2input.ErrConflict
	}
	reference := inputs[0].reference
	if !reference.person.originalPinned {
		return zero, v2input.ErrInvalid
	}
	if err := s.validateNativeInputTransferReference(ctx, who, request, c, reference); err != nil {
		return zero, err
	}
	expected, err := canonical.Marshal(c.Expected)
	if err != nil {
		return zero, err
	}
	tasks := make([]v2input.FrozenSemanticTask, n)
	for i, input := range inputs {
		target := input.target
		q := input.qualified
		if target.ordinal != i || target.binding != targets[i] || target.binding != published.Inventory.Targets[i] || target.protection.Root != c.TargetProtection[i] || !sameNativeInputTransferDocuments(ctx, reference, input.reference) {
			return zero, v2input.ErrConflict
		}
		if err := s.verifyNativeTransferTarget(ctx, who, request, reference, target); err != nil {
			return zero, err
		}
		if q.pin != c.Qualification || q.tenant != request.TenantID || q.project != s.ProjectID || q.actor != who.UserID || q.session != who.SessionID || q.expected != string(expected) || q != inputs[0].qualified {
			return zero, v2input.ErrConflict
		}
		if err := validateTransferMeasuredCosts(c.Costs, q.metrics); err != nil {
			return zero, err
		}
		profile, err := nativeAllTargetTransferProfile(request, reference, target, c)
		if err != nil {
			return zero, err
		}
		if string(profile) != input.profile {
			return zero, v2input.ErrConflict
		}
		expectedDocs := v2input.SemanticPinnedDocuments{Reference: reference.reference, SelectedMask: reference.selected, SourceProtection: reference.protection, Target: json.RawMessage(target.image), TargetProtection: target.protection.Body}
		if !reflect.DeepEqual(expectedDocs, documents[i]) {
			return zero, v2input.ErrConflict
		}
		parameters := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: request.Intent.Reference, Target: target.binding, ModelLockSHA: c.Qualification.ModelLockSHA, Calibration: c.Qualification.Calibration}
		costs := c.Costs
		if len(c.sceneEvidence) > 0 {
			if string(input.scene) != string(c.sceneEvidence[i]) {
				return zero, v2input.ErrConflict
			}
			_, costs, err = semanticSceneTransferProfile(request.Intent.Reference, target.binding, reference.reference, json.RawMessage(target.image), input.scene, c)
			if err != nil {
				return zero, err
			}
		} else if len(input.scene) > 0 {
			return zero, v2input.ErrConflict
		}
		tasks[i], err = v2input.CompileSemanticTransferTaskFromDocuments(request.WorkflowID, i, parameters, request.Intent, params[i], v2input.SemanticTransferDependencies{QualifiedCosts: costs, Scene: input.scene, SourceProtection: c.SourceProtection, TargetProtection: target.protection.Root}, documents[i], profile)
		if err != nil {
			return zero, err
		}
	}
	core, err := CompileWorkflowPlan(request, who.SessionID, published, snapshot, snapshotBody, tasks, c.Qualification)
	if err != nil {
		return zero, err
	}
	core.nativeTargetsRequired = true
	core.sourcesRequired = true
	core.transferQualificationRequired = true
	q := inputs[0].qualified
	core.transferQualification = &q
	owned := make([]nativeTransferTargetProof, n)
	for i, input := range inputs {
		t := input.target
		owned[i] = t
		core.sources = append(core.sources, reference.person.source.source, t.source)
		core.protection = append(core.protection, protectionProof{Binding: request.Intent.Reference, Root: c.SourceProtection, Child: reference.person.source.protection.verified.Pixels}, protectionProof{Binding: t.binding, Root: t.protection.Root, Child: t.protection.Pixels})
	}
	core.config, core.configSHA, err = compileNativeAllTargetTransferConfig(ctx, who, request, core, reference, owned, c)
	if err != nil {
		return zero, err
	}
	return nativeAllTargetTransferWorkflowPlan{core: core, reference: reference, targets: owned}, ctx.Err()
}

func (s SQLTransferSourceReader) validateNativeAllTargetTransferPlanScope(ctx context.Context, who auth.Identity, request Request, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	core := plan.core
	n := len(core.tasks)
	if !core.nativeTargetsRequired || core.nativeReferenceRequired || core.nativeReference != nil || !core.sourcesRequired || !core.transferQualificationRequired || core.transferQualification == nil || len(core.person) != 0 || n < 1 || n > 5000 || n != len(core.frozen) || n != len(plan.targets) || len(core.sources) != 2*n || len(core.protection) != 2*n {
		return v2input.ErrInvalid
	}
	if len(c.sceneEvidence) > 0 && len(c.sceneEvidence) != n {
		return v2input.ErrInvalid
	}
	inputs := make([]nativeAllTargetTransferTaskInput, n)
	documents := make([]v2input.SemanticPinnedDocuments, n)
	params := make([]v2input.Ref, n)
	for i, t := range plan.targets {
		pin, err := core.frozen[i].ProtectionPin()
		if err != nil {
			return err
		}
		if pin.Ordinal != i {
			return v2input.ErrConflict
		}
		params[i] = pin.Params
		draft, err := core.frozen[i].DraftTask()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(draft, core.tasks[i]) {
			return v2input.ErrConflict
		}
		inputs[i] = nativeAllTargetTransferTaskInput{reference: plan.reference, target: t, qualified: *core.transferQualification, profile: string(draft.ExecutionProfile)}
		// Revalidation must retain the same authenticated scene evidence as initial freezing.
		if len(c.sceneEvidence) > 0 {
			inputs[i].scene = append(json.RawMessage(nil), c.sceneEvidence[i]...)
		}
		documents[i] = v2input.SemanticPinnedDocuments{Reference: plan.reference.reference, SelectedMask: plan.reference.selected, SourceProtection: plan.reference.protection, Target: json.RawMessage(t.image), TargetProtection: t.protection.Body}
	}
	expected, err := s.compileNativeAllTargetTransferWorkflowPlan(ctx, who, request, PublishedIntent{Ref: core.intent, Inventory: core.inventory}, core.snapshotRef, core.snapshotBody, params, inputs, documents, c)
	if err != nil {
		return err
	}
	e := expected.core
	if core.tenant != e.tenant || core.actor != e.actor || core.run != e.run || core.workflow != e.workflow || core.project != e.project || core.snapshot != e.snapshot || core.requestSHA != e.requestSHA || core.policySHA != e.policySHA || core.configSHA != e.configSHA || string(core.config) != string(e.config) || !reflect.DeepEqual(core.tasks, e.tasks) || !reflect.DeepEqual(core.sources, e.sources) || !reflect.DeepEqual(core.protection, e.protection) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
