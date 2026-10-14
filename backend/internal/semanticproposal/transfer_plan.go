package semanticproposal

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// TransferTaskInputs contains scoped, byte-verified source and mask inputs.
// Preparing these inputs and validating qualification documents is required
// before this compiler is called; references alone are not that evidence.
type TransferTaskInputs struct {
	nativeReference               *nativeSourceContinuationProof
	referenceSource, targetSource *sourcePreparationProof
	sourcePerson, targetPerson    *personProtectionProof
	qualification                 *transferQualificationPreparationProof
	Dependencies                  v2input.SemanticTransferDependencies
	Profile                       json.RawMessage
}

// CompileTransferWorkflowPlan binds publication ordinal to target ordinal and
// delegates all frozen task/snapshot/qualification checks to the core compiler.
func CompileTransferWorkflowPlan(request Request, session uuid.UUID, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, parameterRefs []v2input.Ref, inputs []TransferTaskInputs, qualification v2input.SemanticQualificationPin) (WorkflowPlan, error) {
	return compileTransferWorkflowPlan(request, session, published, snapshot, snapshotBody, parameterRefs, inputs, qualification, nil)
}

// CompileTransferWorkflowPlanFromDocuments derives dimensions, encoding, child
// references and mask geometry from exact hash-pinned WorkingImage/MaskRef bytes.
// Catalog authority, ICC and NPY payload validation remain resolver obligations.
func CompileTransferWorkflowPlanFromDocuments(request Request, session uuid.UUID, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, parameterRefs []v2input.Ref, inputs []TransferTaskInputs, qualification v2input.SemanticQualificationPin, documents []v2input.SemanticPinnedDocuments) (WorkflowPlan, error) {
	if len(documents) == 0 || len(documents) != len(inputs) {
		return WorkflowPlan{}, v2input.ErrInvalid
	}
	return compileTransferWorkflowPlan(request, session, published, snapshot, snapshotBody, parameterRefs, inputs, qualification, documents)
}

func compileTransferWorkflowPlan(request Request, session uuid.UUID, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, parameterRefs []v2input.Ref, inputs []TransferTaskInputs, qualification v2input.SemanticQualificationPin, documents []v2input.SemanticPinnedDocuments) (WorkflowPlan, error) {
	targets, err := BindTargets(request, snapshot, snapshotBody)
	if err != nil {
		return WorkflowPlan{}, err
	}
	if len(targets) == 0 || len(targets) != len(parameterRefs) || len(targets) != len(inputs) || len(targets) != len(published.Inventory.Targets) {
		return WorkflowPlan{}, v2input.ErrInvalid
	}
	tasks := make([]v2input.FrozenSemanticTask, len(targets))
	for ordinal, target := range targets {
		if target != published.Inventory.Targets[ordinal] {
			return WorkflowPlan{}, v2input.ErrInvalid
		}
		params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: request.Intent.Reference, Target: target, ModelLockSHA: qualification.ModelLockSHA, Calibration: qualification.Calibration}
		var task v2input.FrozenSemanticTask
		var err error
		if documents != nil {
			task, err = v2input.CompileSemanticTransferTaskFromDocuments(request.WorkflowID, ordinal, params, request.Intent, parameterRefs[ordinal], inputs[ordinal].Dependencies, documents[ordinal], inputs[ordinal].Profile)
		} else {
			task, err = v2input.CompileSemanticTransferTask(request.WorkflowID, ordinal, params, request.Intent, parameterRefs[ordinal], inputs[ordinal].Dependencies, inputs[ordinal].Profile)
		}
		if err != nil {
			return WorkflowPlan{}, err
		}
		tasks[ordinal] = task
	}
	plan, err := CompileWorkflowPlan(request, session, published, snapshot, snapshotBody, tasks, qualification)
	if err != nil {
		return WorkflowPlan{}, err
	}
	for i := range inputs {
		proof := inputs[i].qualification
		if proof == nil {
			if plan.transferQualification != nil {
				return WorkflowPlan{}, v2input.ErrInvalid
			}
			continue
		}
		if i > 0 && plan.transferQualification == nil {
			return WorkflowPlan{}, v2input.ErrInvalid
		}
		if proof.tenant != request.TenantID || proof.project != published.Inventory.Scope.ProjectID || proof.actor != plan.actor || proof.session != session || proof.pin != qualification {
			return WorkflowPlan{}, v2input.ErrInvalid
		}
		if plan.transferQualification == nil {
			plan.transferQualificationRequired = true
			owned := *proof
			plan.transferQualification = &owned
		} else if *plan.transferQualification != *proof {
			return WorkflowPlan{}, v2input.ErrConflict
		}
	}
	for i, target := range targets {
		reference, destination := inputs[i].referenceSource, inputs[i].targetSource
		if reference == nil && destination == nil {
			if len(plan.sources) > 0 {
				return WorkflowPlan{}, v2input.ErrInvalid
			}
			continue
		}
		if reference == nil || destination == nil || (i > 0 && len(plan.sources) == 0) || reference.binding != request.Intent.Reference || destination.binding != target {
			return WorkflowPlan{}, v2input.ErrInvalid
		}
		for _, proof := range []*sourcePreparationProof{reference, destination} {
			if proof.tenant != request.TenantID || proof.project != plan.project || proof.actor != plan.actor || proof.session != session {
				return WorkflowPlan{}, v2input.ErrInvalid
			}
			if _, err := marshalSourceEvidencePins(*proof); err != nil {
				return WorkflowPlan{}, err
			}
			plan.sourcesRequired = true
			plan.sources = append(plan.sources, *proof)
		}
	}
	if err := bindNativeTransferPlanReference(request, session, &plan, inputs, documents); err != nil {
		return WorkflowPlan{}, err
	}
	for i, target := range targets {
		source, destination := inputs[i].sourcePerson, inputs[i].targetPerson
		if plan.nativeReferenceRequired {
			nativePerson := plan.nativeReference.native.protection.person
			if source != nil || destination == nil || destination.Binding != target || destination.Protection != inputs[i].Dependencies.TargetProtection || destination.Expected != nativePerson.expected || destination.Qualification != nativePerson.qualification || destination.Evidence == "" || destination.Count < 0 {
				return WorkflowPlan{}, v2input.ErrInvalid
			}
			plan.person = append(plan.person, *destination)
			continue
		}
		if source == nil && destination == nil {
			continue
		}
		if source == nil || destination == nil || documents == nil || source.Binding != request.Intent.Reference || destination.Binding != target || source.Protection != inputs[i].Dependencies.SourceProtection || destination.Protection != inputs[i].Dependencies.TargetProtection || source.Qualification == "" || source.Qualification != destination.Qualification || source.Evidence == "" || destination.Evidence == "" || source.Expected == "" || source.Expected != destination.Expected || source.Report != destination.Report || source.Count < 0 || destination.Count < 0 {
			return WorkflowPlan{}, v2input.ErrInvalid
		}
		plan.person = append(plan.person, *source, *destination)
	}
	personCount := 2 * len(targets)
	if plan.nativeReferenceRequired {
		personCount = len(targets)
	}
	if len(plan.person) != 0 && len(plan.person) != personCount {
		return WorkflowPlan{}, v2input.ErrInvalid
	}
	if documents != nil {
		for i, target := range targets {
			for _, value := range []struct {
				binding     v2input.SessionVersionBinding
				root        v2input.Ref
				mask, image json.RawMessage
			}{
				{request.Intent.Reference, inputs[i].Dependencies.SourceProtection, documents[i].SourceProtection, documents[i].Reference},
				{target, inputs[i].Dependencies.TargetProtection, documents[i].TargetProtection, documents[i].Target},
			} {
				child, err := v2input.ResolveSemanticMaskPixels(value.root, value.mask, value.binding, value.image, true)
				if err != nil {
					return WorkflowPlan{}, err
				}
				plan.protection = append(plan.protection, protectionProof{Binding: value.binding, Root: value.root, Child: child})
			}
		}
	}
	return plan, nil
}
