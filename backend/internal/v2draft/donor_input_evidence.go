package v2draft

import (
	"context"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type DonorManualProducer struct {
	RootRef               v2input.Ref
	RootRaw               []byte
	ParamsRef             v2input.Ref
	ParamsRaw, VectorsRaw []byte
}

type PreparedDonorInputEvidence struct {
	Entries       []v2input.Entry
	Masks         TaskInputEvidence
	Faces         LoadedDonorFaceEvidence
	Images        []DonorImageObject
	calibration   *donorCalibrationEvidence
	qualification *v2portrait.LocatedDonorQualification
}

// PrepareLoadedDonorInputEvidence joins actual mask traversal and face reads
// with independently authorized image occurrences. Entries are sorted/deduped;
// every scoped occurrence remains in its evidence portion. Operation Params
// publication, pinned calibration, binary mask replay and admission are separate.
func (s ClosureResolver) PrepareLoadedDonorInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	loaded LoadedDonorTaskPlan, donorCatalog ArtifactCatalog, manual DonorManualProducer, minEdge float64) (PreparedDonorInputEvidence, error) {
	var zero PreparedDonorInputEvidence
	if math.IsNaN(minEdge) || math.IsInf(minEdge, 0) || minEdge <= 0 {
		return zero, v2input.ErrInvalid
	}
	loaded = ownLoadedDonorTaskPlan(loaded)
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	// Own producer buffers before invoking catalog/object callbacks.
	manual.RootRaw = appendOptionalBytes(manual.RootRaw)
	manual.ParamsRaw = appendOptionalBytes(manual.ParamsRaw)
	manual.VectorsRaw = appendOptionalBytes(manual.VectorsRaw)
	masks, err := s.ResolveLoadedDonorManualMaskEvidence(ctx, who, tenant, loaded, manual.RootRef, manual.RootRaw, manual.ParamsRef, manual.ParamsRaw, manual.VectorsRaw)
	if err != nil {
		return zero, err
	}
	faces, err := LoadDonorFaceEvidence(ctx, who, tenant, loaded, s.Objects, s.Catalog, donorCatalog, minEdge)
	if err != nil {
		return zero, err
	}
	// Repeat physical catalog occurrences, including shared arrays under both
	// versions. A cached loaded evidence object is not a transferable scope grant.
	images, err := ResolveDonorImageObjects(ctx, who, tenant, params, loaded.Images.SourceRaw, loaded.Images.DonorRaw, s.Catalog, donorCatalog)
	if err != nil {
		return zero, err
	}
	inventory := append([]v2input.Entry(nil), masks.Entries...)
	inventory = append(inventory, loaded.Plan.PhysicalImageInputs...)
	inventory = append(inventory, v2input.Entry{Ref: params.Donor.Source, Contract: "WorkingImage"},
		v2input.Entry{Ref: params.Confirmation, Contract: "DonorConfirmation"},
		v2input.Entry{Ref: params.SourceFaces, Contract: "FaceObservations"}, v2input.Entry{Ref: params.DonorFaces, Contract: "FaceObservations"})
	entries, err := uniqueTaskInputs(inventory)
	if err != nil {
		return zero, err
	}
	// Reserve one direct-input slot for the operation Params artifact.
	if len(entries) > 126 {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PreparedDonorInputEvidence{Entries: entries, Masks: masks, Faces: faces, Images: images}, nil
}

func ownLoadedDonorTaskPlan(loaded LoadedDonorTaskPlan) LoadedDonorTaskPlan {
	loaded.Plan.Params = appendOptionalBytes(loaded.Plan.Params)
	loaded.Plan.Profile = appendOptionalBytes(loaded.Plan.Profile)
	loaded.Plan.Inputs = append([]v2input.Ref(nil), loaded.Plan.Inputs...)
	loaded.Plan.PhysicalImageInputs = append([]v2input.Entry(nil), loaded.Plan.PhysicalImageInputs...)
	loaded.Plan.PhysicalImageOccurrences = append([]v2portrait.DonorImageOccurrence(nil), loaded.Plan.PhysicalImageOccurrences...)
	loaded.Images.SourceRaw = appendOptionalBytes(loaded.Images.SourceRaw)
	loaded.Images.DonorRaw = appendOptionalBytes(loaded.Images.DonorRaw)
	loaded.Images.Physical = append([]DonorImageObject(nil), loaded.Images.Physical...)
	return loaded
}
