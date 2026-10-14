package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// TransferPersonProtection is trusted deployment policy for the person producer.
// It is independent of semantic.transfer qualification and caller mask refs.
type TransferPersonProtection struct {
	Report   v2input.Ref
	Expected v2qualification.Expected
}

// TransferPreparation is deployment-selected; protection refs must be selected
// by the required protection policy, not copied from caller input.
type TransferPreparation struct {
	legacyNativeQueueOmitted bool
	legacyAllTargetQueueOmitted bool
	Qualification              v2input.SemanticQualificationPin
	Expected                   v2qualification.Expected
	Color                      TransferColorProfile
	Costs                      v2input.SemanticQualifiedCosts
	RAM, VRAM, Scratch, Output int64
	MetadataBytes              int64
	// Nil retains the internal blocked-draft scaffold only. Public create requires
	// complete person/defect policy authority and final admission inventory locks.
	PersonProtection *TransferPersonProtection
	SourceProtection v2input.Ref
	TargetProtection []v2input.Ref
}

// PrepareTransferInputs validates actual runtime documents and arrays before
// parameter publication/task compilation. It creates no SQL draft or ticket.
func (s SQLTransferSourceReader) PrepareTransferInputs(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, configuration TransferPreparation) ([]TransferTaskInputs, []v2input.SemanticPinnedDocuments, error) {
	return s.prepareTransferInputs(ctx, who, request, published, configuration, nil)
}

// Native preparation retains its distinct reference authority; targets use the
// same required scoped source/person/protection/cost checks as ordinary transfer.
func (s SQLTransferSourceReader) prepareNativeTransferInputs(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, configuration TransferPreparation, reference nativeTransferReferenceDocuments) ([]TransferTaskInputs, []v2input.SemanticPinnedDocuments, error) {
	return s.prepareTransferInputs(ctx, who, request, published, configuration, &reference)
}

func (s SQLTransferSourceReader) prepareTransferInputs(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, configuration TransferPreparation, native *nativeTransferReferenceDocuments) ([]TransferTaskInputs, []v2input.SemanticPinnedDocuments, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	configuration = ownTransferPreparation(configuration)
	if err := validateTransferConfiguration(configuration); err != nil {
		return nil, nil, err
	}
	var nativeProof *nativeSourceContinuationProof
	if native != nil {
		owned, err := s.validateNativeTransferReference(who, request, configuration, *native)
		if err != nil {
			return nil, nil, err
		}
		nativeProof = &owned
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
	if native != nil {
		if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(nativeProof.source.pins), len(nativeProof.native.defect.policy.body), len(nativeProof.native.defect.faceSet), len(nativeProof.native.defect.parserSet), len(nativeProof.native.protection.person.evidence), len(nativeProof.native.protection.person.qualification), len(nativeProof.native.protection.person.expected)); err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, len(native.reference), len(native.selected), len(native.protection), len(nativeProof.native.defect.defect.expected)); err != nil {
			return nil, nil, err
		}
		for _, value := range nativeProof.native.defect.reports.expected {
			if err := chargeMetadata(&retained, configuration.MetadataBytes, len(value)); err != nil {
				return nil, nil, err
			}
		}
		for _, face := range nativeProof.native.defect.faces {
			if err := chargeMetadata(&retained, configuration.MetadataBytes, 2048, len(face.expected)); err != nil {
				return nil, nil, err
			}
		}
		for _, parser := range nativeProof.native.defect.parsers {
			if err := chargeMetadata(&retained, configuration.MetadataBytes, 2048, len(parser.evidence.expected)); err != nil {
				return nil, nil, err
			}
		}
	}
	var reference v2input.SemanticSourceMetadata
	var referenceProof sourcePreparationProof
	var referenceBody, selectedBody json.RawMessage
	var sourceProtection VerifiedProtection
	var sourcePerson *personProtectionProof
	if native != nil {
		reference, err = v2input.ResolveSemanticSourceMetadata(request.Intent.Reference, native.reference)
		if err != nil {
			return nil, nil, err
		}
		if err = configuration.Color.VerifyWorkingImage(native.reference); err != nil {
			return nil, nil, err
		}
		referenceProof = nativeProof.source
		referenceBody = append(json.RawMessage(nil), native.reference...)
		selectedBody = append(json.RawMessage(nil), native.selected...)
		selectedPixels, err := v2input.ResolveSemanticMaskPixels(request.Intent.ReferenceMask, selectedBody, request.Intent.Reference, referenceBody, false)
		if err != nil || selectedPixels != nativeProof.native.protection.selection.pixels {
			return nil, nil, v2input.ErrInvalid
		}
		protectedPixels, err := v2input.ResolveSemanticMaskPixels(configuration.SourceProtection, native.protection, request.Intent.Reference, referenceBody, true)
		if err != nil || protectedPixels != nativeProof.native.protection.selection.registered.objects[1].ref {
			return nil, nil, v2input.ErrInvalid
		}
		sourceProtection = VerifiedProtection{Root: configuration.SourceProtection, Pixels: protectedPixels, Body: append(json.RawMessage(nil), native.protection...)}
	} else {
		reference, referenceProof, err = s.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, configuration.Color, false)
		if err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(referenceProof.pins)); err != nil {
			return nil, nil, err
		}
		referenceBody, err = s.Read(ctx, who, request.TenantID, request.Intent.Reference)
		if err != nil {
			return nil, nil, err
		}
		selectedBody, err = s.ReadSelectedMask(ctx, who, request)
		if err != nil {
			return nil, nil, err
		}
		sourceProtection, err = s.ReadProtection(ctx, who, request.TenantID, request.Intent.Reference, configuration.SourceProtection)
		if err != nil {
			return nil, nil, err
		}
		if configuration.PersonProtection != nil {
			policy := configuration.PersonProtection
			proof, err := s.selectPersonProtectionProof(ctx, who, request.TenantID, request.Intent.Reference, sourceProtection.Root, policy.Report, policy.Expected)
			if err != nil {
				return nil, nil, err
			}
			sourcePerson = &proof
		}
		if _, err := s.VerifySelectionSeparation(ctx, who, request, sourceProtection.Root); err != nil {
			return nil, nil, err
		}
	}
	// Charge fixed per-item metadata before allocating the batch slices.
	if err := chargeMetadata(&retained, configuration.MetadataBytes, len(targets)*1024); err != nil {
		return nil, nil, err
	}
	inputs := make([]TransferTaskInputs, len(targets))
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
		profile, err := v2input.CompileSemanticTransferProfile(reference.Width, reference.Height, metadata.Width, metadata.Height, reference.Gamma, metadata.Gamma, configuration.Costs, configuration.RAM, configuration.VRAM, configuration.Scratch, configuration.Output)
		if err != nil {
			return nil, nil, err
		}
		if err := chargeMetadata(&retained, configuration.MetadataBytes, len(referenceBody), len(body), len(selectedBody), len(sourceProtection.Body), len(protection.Body), len(profile)); err != nil {
			return nil, nil, err
		}
		if sourcePerson != nil && targetPerson != nil {
			if err := chargeMetadata(&retained, configuration.MetadataBytes, len(sourcePerson.Expected), len(targetPerson.Expected), len(sourcePerson.Evidence), len(targetPerson.Evidence), len(sourcePerson.Qualification), len(targetPerson.Qualification)); err != nil {
				return nil, nil, err
			}
		}
		if sourcePerson != nil && targetPerson != nil {
			for _, proof := range [2]*personProtectionProof{sourcePerson, targetPerson} {
				p := proof.protectionEvidence
				if err := chargeMetadata(&retained, configuration.MetadataBytes, 1024, len(p.bucket), len(p.root.object.Key), len(p.root.object.VersionID), len(p.pixels.object.Key), len(p.pixels.object.VersionID)); err != nil {
					return nil, nil, err
				}
			}
		}
		inputs[i] = TransferTaskInputs{nativeReference: nativeProof, Dependencies: v2input.SemanticTransferDependencies{QualifiedCosts: configuration.Costs, SourceProtection: sourceProtection.Root, TargetProtection: protection.Root}, Profile: profile, sourcePerson: sourcePerson, targetPerson: targetPerson, qualification: &qualification, referenceSource: &referenceProof, targetSource: &targetProof}
		documents[i] = v2input.SemanticPinnedDocuments{Reference: append([]byte(nil), referenceBody...), Target: body, SelectedMask: append([]byte(nil), selectedBody...), SourceProtection: append([]byte(nil), sourceProtection.Body...), TargetProtection: protection.Body}
	}
	// Recheck retained original locations after all later object IO. This
	// returns no partial batch on drift; final SQL admission must still lock them.
	checkedSources := map[sourcePreparationProof]bool{}
	for _, input := range inputs {
		for _, proof := range []*sourcePreparationProof{input.referenceSource, input.targetSource} {
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
	return inputs, documents, nil
}
