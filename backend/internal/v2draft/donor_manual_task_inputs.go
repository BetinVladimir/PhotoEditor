package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ResolveLoadedDonorManualMaskEvidence reuses the original manual producer
// traversal for the loaded source/eyes/protected pins. It is only the source mask
// portion of closure: donor image and face/confirmation evidence stay separate.
func (s ClosureResolver) ResolveLoadedDonorManualMaskEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	loaded LoadedDonorTaskPlan, rootRef v2input.Ref, rootRaw []byte, producerRef v2input.Ref, producerRaw, vectorsRaw []byte) (TaskInputEvidence, error) {
	var zero TaskInputEvidence
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	// Producer documents must not impersonate any JSON root or image array.
	for _, ref := range []v2input.Ref{rootRef, producerRef} {
		for _, direct := range loaded.Plan.Inputs {
			if ref.ArtifactID == direct.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
		for _, physical := range loaded.Plan.PhysicalImageInputs {
			if ref.ArtifactID == physical.Ref.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
	}
	result, err := s.ResolveManualRasterTaskInputEvidence(ctx, who, tenant, params.Base, append([]byte(nil), loaded.Images.SourceRaw...), rootRef, append([]byte(nil), rootRaw...), producerRef, append([]byte(nil), producerRaw...), appendOptionalBytes(vectorsRaw), params.EyesMask, params.Protected)
	if err != nil {
		return zero, err
	}
	expected := []v2input.Entry{{Ref: params.Base.Source, Contract: "WorkingImage"}, {Ref: params.Donor.Source, Contract: "WorkingImage"},
		{Ref: params.EyesMask, Contract: "MaskRef"}, {Ref: params.Protected, Contract: "MaskRef"}, {Ref: params.Confirmation, Contract: "DonorConfirmation"},
		{Ref: params.SourceFaces, Contract: "FaceObservations"}, {Ref: params.DonorFaces, Contract: "FaceObservations"}}
	expected = append(expected, loaded.Plan.PhysicalImageInputs...)
	if _, err := uniqueTaskInputs(append(expected, result.Entries...)); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}

func appendOptionalBytes(raw []byte) []byte {
	if raw == nil {
		return nil
	}
	return append([]byte{}, raw...)
}
