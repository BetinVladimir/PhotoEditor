package v2draft

import (
	"context"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// VerifyLoadedDonorFaces joins exact plan/image pins with confirmation and face
// bytes. The caller still needs catalog provenance for all three documents and
// a qualified calibration; successful geometry is not graph authorization.
func VerifyLoadedDonorFaces(ctx context.Context, loaded LoadedDonorTaskPlan, confirmationRaw, sourceFacesRaw, donorFacesRaw []byte,
	minEdge float64) (v2portrait.DonorFaceGeometry, error) {
	var zero v2portrait.DonorFaceGeometry
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	geometry, err := v2portrait.VerifyOpenEyesFacesFromManifests(params, confirmationRaw, sourceFacesRaw, donorFacesRaw, loaded.Images.SourceRaw, loaded.Images.DonorRaw, minEdge)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return geometry, nil
}

type LoadedDonorFaceEvidence struct {
	ConfirmationRaw, SourceFacesRaw, DonorFacesRaw []byte
	Geometry                                       v2portrait.DonorFaceGeometry
	minEdge                                        float64
}

// LoadDonorFaceEvidence reads proof first, then each face document through the
// corresponding version catalog anchored to that proof. No partial evidence is
// returned on authorization, object integrity or geometry failure.
func LoadDonorFaceEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, loaded LoadedDonorTaskPlan,
	objects v2input.SessionAnalysisObjectReader, baseCatalog, donorCatalog ArtifactCatalog, minEdge float64) (LoadedDonorFaceEvidence, error) {
	var zero LoadedDonorFaceEvidence
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	if math.IsNaN(minEdge) || math.IsInf(minEdge, 0) || minEdge <= 0 {
		return zero, v2input.ErrInvalid
	}
	if objects == nil || baseCatalog == nil || donorCatalog == nil || tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	sourceRaw := append([]byte(nil), loaded.Images.SourceRaw...)
	donorRaw := append([]byte(nil), loaded.Images.DonorRaw...)
	binding := params.Base
	object, err := baseCatalog.Lookup(ctx, who, tenant, LayerRef{Contract: "DonorConfirmation", Ref: params.Confirmation, ParentRef: binding.Source, ExpectedBinding: &binding, CanvasHash: binding.CanvasHash})
	if err != nil {
		return zero, err
	}
	if object.Bytes > 65536 {
		return zero, v2input.ErrInvalid
	}
	proof, err := readJSONArtifact(ctx, objects, object, params.Confirmation)
	if err != nil {
		return zero, err
	}
	if _, err := v2portrait.VerifyOpenEyesConfirmation(params, proof); err != nil {
		return zero, err
	}
	raws := make([][]byte, 2)
	for i, b := range []v2input.SessionVersionBinding{params.Base, params.Donor} {
		catalog := baseCatalog
		ref := params.SourceFaces
		if i == 1 {
			catalog = donorCatalog
			ref = params.DonorFaces
		}
		pin := b
		object, err := catalog.Lookup(ctx, who, tenant, LayerRef{Contract: "DonorFaceObservations", Ref: ref, ParentRef: params.Confirmation, ExpectedBinding: &pin, CanvasHash: b.CanvasHash})
		if err != nil {
			return zero, err
		}
		if object.Bytes > 1048576 {
			return zero, v2input.ErrInvalid
		}
		raws[i], err = readJSONArtifact(ctx, objects, object, ref)
		if err != nil {
			return zero, err
		}
	}
	geometry, err := v2portrait.VerifyOpenEyesFacesFromManifests(params, proof, raws[0], raws[1], sourceRaw, donorRaw, minEdge)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return LoadedDonorFaceEvidence{ConfirmationRaw: proof, SourceFacesRaw: raws[0], DonorFacesRaw: raws[1], Geometry: geometry, minEdge: minEdge}, nil
}
