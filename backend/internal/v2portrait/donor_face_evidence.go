package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
)

type DonorFaceGeometry struct {
	SourceLandmarks, DonorLandmarks [68][2]float64
	SourceDiagonal, DonorDiagonal   float64
}

// VerifyOpenEyesFaces verifies pinned pre-edit observations and selected face
// geometry. Sizes must come from verified WorkingImage manifests and minEdge
// from pinned calibration. Current consent and catalog scope are separate gates.
func VerifyOpenEyesFaces(params OpenEyesParams, confirmationRaw, sourceFacesRaw, donorFacesRaw []byte,
	sourceSize, donorSize [2]int64, minEdge float64) (DonorFaceGeometry, error) {
	var zero DonorFaceGeometry
	if math.IsNaN(minEdge) || math.IsInf(minEdge, 0) || minEdge <= 0 {
		return zero, v2input.ErrInvalid
	}
	proof, err := VerifyOpenEyesConfirmation(params, confirmationRaw)
	if err != nil {
		return zero, err
	}
	raws := [][]byte{sourceFacesRaw, donorFacesRaw}
	refs := []v2input.Ref{params.SourceFaces, params.DonorFaces}
	bindings := []v2input.SessionVersionBinding{params.Base, params.Donor}
	observations := []uuid.UUID{proof.SourceObservationID, *proof.DonorObservationID}
	faces := []uuid.UUID{params.SourceFaceID, params.DonorFaceID}
	sizes := [][2]int64{sourceSize, donorSize}
	var result DonorFaceGeometry
	for i, raw := range raws {
		w, h := sizes[i][0], sizes[i][1]
		if w < 1 || h < 1 || w > 200000000 || h > 200000000/w {
			return zero, v2input.ErrInvalid
		}
		b := bindings[i]
		expected := v2observation.Binding{AssetVersionID: b.AssetVersionID, Source: v2observation.ArtifactRef{ArtifactID: b.Source.ArtifactID, SHA256: b.Source.SHA256}, CanvasHash: b.CanvasHash}
		faceID, err := v2observation.ResolveFaceID(raw, refs[i].SHA256, expected, observations[i])
		if err != nil || faceID != faces[i] {
			return zero, v2input.ErrInvalid
		}
		var evidence v2observation.Evidence
		if json.Unmarshal(raw, &evidence) != nil {
			return zero, v2input.ErrInvalid
		}
		for _, face := range evidence.Faces {
			if face.ObservationID != observations[i] {
				continue
			}
			box := face.Box
			ew, eh := (*box.X1-*box.X0)*float64(w), (*box.Y1-*box.Y0)*float64(h)
			if math.Min(ew, eh) < minEdge {
				return zero, v2input.ErrInvalid
			}
			var points [68][2]float64
			for j, point := range face.Landmarks {
				if *point.X < *box.X0 || *point.X > *box.X1 || *point.Y < *box.Y0 || *point.Y > *box.Y1 {
					return zero, v2input.ErrInvalid
				}
				points[j] = [2]float64{*point.X * float64(w), *point.Y * float64(h)}
			}
			if i == 0 {
				result.SourceLandmarks = points
				result.SourceDiagonal = math.Hypot(ew, eh)
			} else {
				result.DonorLandmarks = points
				result.DonorDiagonal = math.Hypot(ew, eh)
			}
		}
	}
	return result, nil
}

// VerifyOpenEyesFacesFromManifests prevents callers from substituting dimensions
// while retaining valid observation refs. Both image pins and color compatibility
// are verified before deriving pixel geometry.
func VerifyOpenEyesFacesFromManifests(params OpenEyesParams, confirmationRaw, sourceFacesRaw, donorFacesRaw,
	sourceRaw, donorRaw []byte, minEdge float64) (DonorFaceGeometry, error) {
	var zero DonorFaceGeometry
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return zero, err
	}
	donor, err := VerifyDonorWorkingImage(params.Donor, donorRaw)
	if err != nil {
		return zero, err
	}
	if source.Color != donor.Color {
		return zero, v2input.ErrInvalid
	}
	return VerifyOpenEyesFaces(params, confirmationRaw, sourceFacesRaw, donorFacesRaw,
		[2]int64{source.Canvas.Width, source.Canvas.Height}, [2]int64{donor.Canvas.Width, donor.Canvas.Height}, minEdge)
}
