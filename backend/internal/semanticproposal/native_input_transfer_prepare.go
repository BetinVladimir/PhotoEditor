package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Distinct staging type prevents passing new-native authority to a compiler
// that only understands imported-origin or generic protection admission.
type nativeInputTransferTaskInputs struct {
	task      TransferTaskInputs
	reference nativeInputTransferReferenceDocuments
}

func (s SQLTransferSourceReader) prepareNativeInputTransferInputs(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, configuration TransferPreparation, native nativeInputTransferReferenceDocuments) ([]nativeInputTransferTaskInputs, []v2input.SemanticPinnedDocuments, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	configuration = ownTransferPreparation(configuration)
	if err := validateTransferConfiguration(configuration); err != nil {
		return nil, nil, err
	}
	if err := s.validateNativeInputTransferReference(ctx, who, request, configuration, native); err != nil {
		return nil, nil, err
	}
	targets := append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)
	protections := append([]v2input.Ref(nil), configuration.TargetProtection...)
	if len(targets) == 0 || len(targets) > 5000 || len(targets) != len(protections) || len(targets) != published.Inventory.Scope.ItemCount || s.ProjectID != published.Inventory.Scope.ProjectID {
		return nil, nil, v2input.ErrInvalid
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, nil, err
	}
	if !sameTransferInventory(before, published.Inventory) {
		return nil, nil, v2input.ErrConflict
	}
	qualification, err := s.readTransferQualificationPreparation(ctx, who, request.TenantID, configuration.Qualification, configuration.Expected)
	if err != nil {
		return nil, nil, err
	}
	if err := validateTransferMeasuredCosts(configuration.Costs, qualification.metrics); err != nil {
		return nil, nil, err
	}
	var retained int64
	if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(qualification.expected)); err != nil {
		return nil, nil, err
	}
	for _, object := range qualification.objects {
		if err := chargeMetadata(&retained, configuration.MetadataBytes, len(qualification.bucket), len(object.object.Key), len(object.object.VersionID)); err != nil {
			return nil, nil, err
		}
	}
	if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(native.person.source.source.pins), len(native.person.policy.body), len(native.person.person.evidence), len(native.person.person.qualification), len(native.person.person.expected), len(native.reference), len(native.selected), len(native.protection)); err != nil {
		return nil, nil, err
	}
	reference, err := v2input.ResolveSemanticSourceMetadata(request.Intent.Reference, native.reference)
	if err != nil {
		return nil, nil, err
	}
	referenceProof := native.person.source.source
	referenceBody := append(json.RawMessage(nil), native.reference...)
	selectedBody := append(json.RawMessage(nil), native.selected...)
	sourceProtection := VerifiedProtection{Root: native.person.source.protection.verified.Root, Pixels: native.person.source.protection.verified.Pixels, Body: append(json.RawMessage(nil), native.protection...)}
	// Charge fixed per-item metadata before allocating the batch slices.
	if err := chargeMetadata(&retained, configuration.MetadataBytes, len(targets)*1024); err != nil {
		return nil, nil, err
	}
	inputs := make([]nativeInputTransferTaskInputs, len(targets))
	documents := make([]v2input.SemanticPinnedDocuments, len(targets))
	for i, target := range targets {
		metadata, targetProof, err := s.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, target, configuration.Color, false)
		if err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(targetProof.pins)); err != nil {
			return nil, nil, err
		}
		body, err := s.Read(ctx, who, request.TenantID, target)
		if err != nil {
			return nil, nil, err
		}
		protection, err := s.ReadProtection(ctx, who, request.TenantID, target, protections[i])
		if err != nil {
			return nil, nil, err
		}
		var targetPerson *personProtectionProof
		if configuration.PersonProtection != nil {
			policy := configuration.PersonProtection
			proof, err := s.selectPersonProtectionProof(ctx, who, request.TenantID, target, protection.Root, policy.Report, policy.Expected)
			if err != nil {
				return nil, nil, err
			}
			targetPerson = &proof
		}
		profile, err := v2input.CompileSemanticTransferProfileForBackend(configuration.Expected.Backend, reference.Width, reference.Height, metadata.Width, metadata.Height, reference.Gamma, metadata.Gamma, configuration.Costs, configuration.RAM, configuration.VRAM, configuration.Scratch, configuration.Output)
		if err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, len(referenceBody), len(body), len(selectedBody), len(sourceProtection.Body), len(protection.Body), len(profile)); err != nil {
			return nil, nil, err
		}
		if targetPerson == nil {
			return nil, nil, v2input.ErrInvalid
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, len(targetPerson.Expected), len(targetPerson.Evidence), len(targetPerson.Qualification)); err != nil {
			return nil, nil, err
		}
		protectionPin := targetPerson.protectionEvidence
		if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(protectionPin.bucket), len(protectionPin.root.object.Key), len(protectionPin.root.object.VersionID), len(protectionPin.pixels.object.Key), len(protectionPin.pixels.object.VersionID)); err != nil {
			return nil, nil, err
		}
		inputs[i] = nativeInputTransferTaskInputs{reference: native, task: TransferTaskInputs{Dependencies: v2input.SemanticTransferDependencies{QualifiedCosts: configuration.Costs, SourceProtection: sourceProtection.Root, TargetProtection: protection.Root}, Profile: profile, targetPerson: targetPerson, qualification: &qualification, referenceSource: &referenceProof, targetSource: &targetProof}}
		documents[i] = v2input.SemanticPinnedDocuments{Reference: append([]byte(nil), referenceBody...), Target: body, SelectedMask: append([]byte(nil), selectedBody...), SourceProtection: append([]byte(nil), sourceProtection.Body...), TargetProtection: protection.Body}
	}
	// Recheck retained original locations after all later object IO. This
	// returns no partial batch on drift; final SQL admission must still lock them.
	checkedSources := map[sourcePreparationProof]bool{}
	for _, input := range inputs {
		for _, proof := range []*sourcePreparationProof{input.task.referenceSource, input.task.targetSource} {
			if proof == nil {
				return nil, nil, v2input.ErrInvalid
			}
			if checkedSources[*proof] {
				continue
			}
			if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, proof.binding, configuration.Color, false, *proof); err != nil {
				return nil, nil, err
			}
			checkedSources[*proof] = true
		}
	}
	currentQualification, err := s.qualificationObjects(ctx, who, request.TenantID, configuration.Qualification)
	if err != nil {
		return nil, nil, err
	}
	if currentQualification[0] != qualification.objects[0] || currentQualification[1] != qualification.objects[1] {
		return nil, nil, v2input.ErrConflict
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return nil, nil, err
	}
	if !sameTransferInventory(before, after) {
		return nil, nil, v2input.ErrConflict
	}
	currentReference, err := s.readNativeInputTransferReferenceDocuments(ctx, who, request, native.person, configuration.Color, configuration.MetadataBytes)
	if err != nil {
		return nil, nil, err
	}
	if !sameNativeInputTransferDocuments(ctx, currentReference, native) {
		return nil, nil, v2input.ErrConflict
	}
	return inputs, documents, nil
}
