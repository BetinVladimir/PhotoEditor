package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

func (s SQLTransferSourceReader) validateNativeInputManualTransferPlanScope(ctx context.Context, who auth.Identity, request Request, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := plan.core
	n := len(p.tasks)
	if p.nativeTargetsRequired || n < 1 || n > 5000 || len(p.frozen) != n || len(p.inventory.Targets) != n || len(p.sources) != 2*n || len(p.protection) != 2*n || len(p.person) != n || p.nativeReferenceRequired || p.nativeReference != nil || !p.sourcesRequired || !p.transferQualificationRequired || p.transferQualification == nil {
		return v2input.ErrInvalid
	}
	if p.tenant != request.TenantID || p.actor != who.UserID || p.actor != request.ActorID || p.run != request.RunID || p.workflow != request.WorkflowID || p.project != s.ProjectID || p.requestSHA != request.RequestSHA {
		return v2input.ErrInvalid
	}
	expectedCore, err := CompileWorkflowPlan(request, who.SessionID, PublishedIntent{Ref: p.intent, Inventory: p.inventory}, p.snapshotRef, p.snapshotBody, p.frozen, configuration.Qualification)
	if err != nil {
		return err
	}
	if configuration.legacyNativeQueueOmitted {
		if err := omitNativeTransferQueue(&expectedCore); err != nil { return err }
	}
	if expectedCore.configSHA != p.configSHA || string(expectedCore.config) != string(p.config) || expectedCore.policySHA != p.policySHA || expectedCore.snapshot != p.snapshot {
		return v2input.ErrConflict
	}
	if err := s.validateNativeInputManualTransferReference(ctx, who, request, configuration, plan.reference); err != nil {
		return err
	}
	if p.transferQualification.pin != configuration.Qualification {
		return v2input.ErrConflict
	}
	if _, _, err := s.marshalTransferQualificationPreparation(ctx, who, request.TenantID, *p.transferQualification, configuration.Expected); err != nil {
		return err
	}
	source := plan.reference.person.source.source
	nativePerson := plan.reference.person.person
	registered := plan.reference.person.source.protection
	for i, target := range p.inventory.Targets {
		draft, err := p.frozen[i].DraftTask()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(draft, p.tasks[i]) {
			return v2input.ErrConflict
		}
		if p.sources[2*i] != source || p.sources[2*i+1].binding != target {
			return v2input.ErrConflict
		}
		destination := p.sources[2*i+1]
		if destination.tenant != request.TenantID || destination.project != s.ProjectID || destination.actor != who.UserID || destination.session != who.SessionID || destination.bucket != s.Bucket {
			return v2input.ErrInvalid
		}
		if _, err := marshalSourceEvidencePins(destination); err != nil {
			return err
		}
		referenceProtection, targetProtection := p.protection[2*i], p.protection[2*i+1]
		pin, err := p.frozen[i].ProtectionPin()
		if err != nil {
			return err
		}
		if pin.Ordinal != i || pin.SourceProtection != referenceProtection.Root || pin.TargetProtection != targetProtection.Root {
			return v2input.ErrConflict
		}
		entries, err := p.frozen[i].InputEntries(ctx)
		if err != nil {
			return err
		}
		wanted := []v2input.Entry{{Ref: pin.Params, Contract: "Params"}, {Ref: p.intent, Contract: "EditIntent"}, {Ref: configuration.Qualification.Calibration, Contract: "Calibration"}, {Ref: request.Intent.ReferenceMask, Contract: "MaskRef"}, {Ref: referenceProtection.Root, Contract: "MaskRef"}, {Ref: targetProtection.Root, Contract: "MaskRef"}, {Ref: plan.reference.selection.pixels, Contract: "NpyArray"}, {Ref: referenceProtection.Child, Contract: "NpyArray"}, {Ref: targetProtection.Child, Contract: "NpyArray"}}
		for _, proof := range []sourcePreparationProof{source, destination} {
			pins, err := decodeSourcePreparationPins(proof)
			if err != nil {
				return err
			}
			for j, object := range pins {
				contract := "NpyArray"
				if j == 0 {
					contract = "WorkingImage"
				}
				wanted = append(wanted, v2input.Entry{Ref: object.Node.Ref, Contract: contract})
			}
		}
		if err := matchNativeInputTransferInventory(entries, wanted); err != nil {
			return err
		}
		person := p.person[i]
		if referenceProtection.Binding != request.Intent.Reference || referenceProtection.Root != registered.verified.Root || referenceProtection.Child != registered.verified.Pixels || targetProtection.Binding != target || person.Binding != target || person.Protection != targetProtection.Root || person.protectionEvidence.root.ref != targetProtection.Root || person.protectionEvidence.pixels.ref != targetProtection.Child || person.Expected != nativePerson.expected || person.Qualification != nativePerson.qualification || person.Report != nativePerson.report || !validPersonPreparationScope(s, who, request.TenantID, target, person) {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}
