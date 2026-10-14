package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Distinct plan retains original native-input authority for its admission path.
// It must not be admitted by the generic or imported-origin WorkflowPlan gates.
type nativeInputManualTransferWorkflowPlan struct {
	core      WorkflowPlan
	reference nativeInputManualTransferReferenceDocuments
}

func (s SQLTransferSourceReader) compileNativeInputManualTransferWorkflowPlan(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, parameterRefs []v2input.Ref, inputs []nativeInputManualTransferTaskInputs, qualification v2input.SemanticQualificationPin, documents []v2input.SemanticPinnedDocuments, configuration TransferPreparation) (nativeInputManualTransferWorkflowPlan, error) {
	var zero nativeInputManualTransferWorkflowPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if who.UserID != request.ActorID || who.SessionID == uuid.Nil || len(inputs) == 0 || len(inputs) > 5000 || len(inputs) != len(documents) || len(inputs) != len(parameterRefs) {
		return zero, v2input.ErrInvalid
	}
	targets, err := BindTargets(request, snapshot, snapshotBody)
	if err != nil {
		return zero, err
	}
	if len(targets) != len(inputs) || len(targets) != len(published.Inventory.Targets) {
		return zero, v2input.ErrInvalid
	}
	reference := inputs[0].reference
	if err := s.validateNativeInputManualTransferReference(ctx, who, request, configuration, reference); err != nil {
		return zero, err
	}
	expectedBody, err := canonical.Marshal(configuration.Expected)
	if err != nil {
		return zero, err
	}
	tasks := make([]v2input.FrozenSemanticTask, len(inputs))
	for i, input := range inputs {
		t := input.task
		target := targets[i]
		if target != published.Inventory.Targets[i] || !sameNativeInputManualTransferDocuments(ctx, input.reference, reference) || t.nativeReference != nil || t.sourcePerson != nil || t.targetPerson == nil || t.referenceSource == nil || t.targetSource == nil || t.qualification == nil {
			return zero, v2input.ErrInvalid
		}
		if *t.referenceSource != reference.person.source.source || t.targetSource.binding != target || t.Dependencies.SourceProtection != reference.person.source.protection.verified.Root || t.targetPerson.Binding != target || t.targetPerson.Protection != t.Dependencies.TargetProtection || t.targetPerson.Expected != reference.person.person.expected || t.targetPerson.Qualification != reference.person.person.qualification || t.targetPerson.Report != reference.person.person.report || t.targetPerson.Evidence == "" || t.targetPerson.Count < 0 {
			return zero, v2input.ErrConflict
		}
		if t.qualification.expected != string(expectedBody) || !validPersonPreparationScope(s, who, request.TenantID, target, *t.targetPerson) {
			return zero, v2input.ErrConflict
		}
		if t.qualification.pin != qualification || t.qualification.tenant != request.TenantID || t.qualification.project != s.ProjectID || t.qualification.actor != who.UserID || t.qualification.session != who.SessionID || *t.qualification != *inputs[0].task.qualification {
			return zero, v2input.ErrConflict
		}
		for _, source := range []*sourcePreparationProof{t.referenceSource, t.targetSource} {
			if source.tenant != request.TenantID || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID {
				return zero, v2input.ErrInvalid
			}
			if _, err := marshalSourceEvidencePins(*source); err != nil {
				return zero, err
			}
		}
		if string(documents[i].Reference) != string(reference.reference) || string(documents[i].SelectedMask) != string(reference.selected) || string(documents[i].SourceProtection) != string(reference.protection) {
			return zero, v2input.ErrConflict
		}
		params := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: request.Intent.Reference, Target: target, ModelLockSHA: qualification.ModelLockSHA, Calibration: qualification.Calibration}
		tasks[i], err = v2input.CompileSemanticTransferTaskFromDocuments(request.WorkflowID, i, params, request.Intent, parameterRefs[i], t.Dependencies, documents[i], t.Profile)
		if err != nil {
			return zero, err
		}
	}
	core, err := CompileWorkflowPlan(request, who.SessionID, published, snapshot, snapshotBody, tasks, qualification)
	if err != nil {
		return zero, err
	}
	if configuration.legacyNativeQueueOmitted {
		if err := omitNativeTransferQueue(&core); err != nil { return zero, err }
	}
	q := *inputs[0].task.qualification
	core.transferQualificationRequired = true
	core.transferQualification = &q
	core.sourcesRequired = true
	for i, input := range inputs {
		core.sources = append(core.sources, *input.task.referenceSource, *input.task.targetSource)
		core.person = append(core.person, *input.task.targetPerson)
		core.protection = append(core.protection, protectionProof{Binding: request.Intent.Reference, Root: reference.person.source.protection.verified.Root, Child: reference.person.source.protection.verified.Pixels})
		child, err := v2input.ResolveSemanticMaskPixels(input.task.Dependencies.TargetProtection, documents[i].TargetProtection, targets[i], documents[i].Target, true)
		if err != nil {
			return zero, err
		}
		core.protection = append(core.protection, protectionProof{Binding: targets[i], Root: input.task.Dependencies.TargetProtection, Child: child})
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputManualTransferWorkflowPlan{core: core, reference: reference}, nil
}
