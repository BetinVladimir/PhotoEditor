package v2draft

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

type BoundDonorInputInventory struct {
	Entries   []v2input.Entry
	SHA256    string
	ParamsRef v2input.Ref
}

// BindDonorParamsInventory validates the prepared portions against the loaded
// plan and adds a distinct published operation Params ref. This is a pure
// inventory compiler, not evidence of publication scope or graph authorization.
func BindDonorParamsInventory(ctx context.Context, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence,
	paramsRef v2input.Ref) (BoundDonorInputInventory, error) {
	var zero BoundDonorInputInventory
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	if !isDocumentRef(paramsRef) || paramsRef.SHA256 != loaded.Plan.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	geometry, err := VerifyLoadedDonorFaces(ctx, loaded, prepared.Faces.ConfirmationRaw, prepared.Faces.SourceFacesRaw, prepared.Faces.DonorFacesRaw, prepared.Faces.minEdge)
	if err != nil {
		return zero, err
	}
	if geometry != prepared.Faces.Geometry || len(prepared.Images) != len(loaded.Plan.PhysicalImageOccurrences) {
		return zero, v2input.ErrInvalid
	}
	for i, image := range prepared.Images {
		if image.Occurrence != loaded.Plan.PhysicalImageOccurrences[i] {
			return zero, v2input.ErrInvalid
		}
	}
	required := map[v2input.Ref]string{params.Base.Source: "WorkingImage", params.EyesMask: "MaskRef", params.Protected: "MaskRef"}
	for _, entry := range prepared.Masks.Entries {
		if contract, ok := required[entry.Ref]; ok {
			if entry.Contract != contract {
				return zero, v2input.ErrInvalid
			}
			delete(required, entry.Ref)
		}
	}
	if len(required) != 0 {
		return zero, v2input.ErrInvalid
	}
	inventory := append([]v2input.Entry(nil), prepared.Masks.Entries...)
	inventory = append(inventory, loaded.Plan.PhysicalImageInputs...)
	inventory = append(inventory, v2input.Entry{Ref: params.Donor.Source, Contract: "WorkingImage"}, v2input.Entry{Ref: params.Confirmation, Contract: "DonorConfirmation"}, v2input.Entry{Ref: params.SourceFaces, Contract: "FaceObservations"}, v2input.Entry{Ref: params.DonorFaces, Contract: "FaceObservations"})
	if prepared.calibration != nil {
		pin := prepared.calibration
		if err := pin.verify(prepared.Faces.minEdge); err != nil {
			return zero, err
		}
		for _, entry := range inventory {
			if entry.Ref.ArtifactID == pin.ref.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
		inventory = append(inventory, v2input.Entry{Ref: pin.ref, Contract: "Calibration"})
	}
	if prepared.qualification != nil {
		pin, err := verifyPreparedDonorQualification(loaded, prepared)
		if err != nil {
			return zero, err
		}
		for _, entry := range inventory {
			if entry.Ref.ArtifactID == pin.Report.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
		inventory = append(inventory, v2input.Entry{Ref: pin.Report, Contract: "Report"})
	}
	expected, err := uniqueTaskInputs(inventory)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(expected, prepared.Entries) || len(expected) > 126 {
		return zero, v2input.ErrInvalid
	}
	for _, entry := range expected {
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	entries, err := uniqueTaskInputs(append(expected, v2input.Entry{Ref: paramsRef, Contract: "Params"}))
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(entries)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return BoundDonorInputInventory{Entries: entries, SHA256: digest, ParamsRef: paramsRef}, nil
}
