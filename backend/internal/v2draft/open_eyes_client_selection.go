package v2draft

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// OpenEyesClientSelection is authored by the editor. Confirmation, identity,
// consent, qualification, runtime and reservation are server-owned values.
type OpenEyesClientSelection struct {
	SchemaVersion     int                           `json:"schema_version"`
	Shoot             uuid.UUID                     `json:"shoot_session_id"`
	Snapshot          uuid.UUID                     `json:"snapshot_id"`
	Ordinal           int                           `json:"ordinal"`
	Base              v2input.SessionVersionBinding `json:"base"`
	Donor             v2input.SessionVersionBinding `json:"donor"`
	Strength          float64                       `json:"strength"`
	EyesMask          v2input.Ref                   `json:"eyes_mask"`
	Protected         v2input.Ref                   `json:"protected"`
	TargetPixel       v2input.Ref                   `json:"target_pixel"`
	ProtectedPixel    v2input.Ref                   `json:"protected_pixel"`
	SourceFaces       v2input.Ref                   `json:"source_faces"`
	DonorFaces        v2input.Ref                   `json:"donor_faces"`
	SourceObservation uuid.UUID                     `json:"source_observation_id"`
	DonorObservation  uuid.UUID                     `json:"donor_observation_id"`
}

func DecodeOpenEyesClientSelection(raw []byte) (OpenEyesClientSelection, error) {
	var zero OpenEyesClientSelection
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var request OpenEyesClientSelection
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 || request.Shoot == uuid.Nil || request.Snapshot == uuid.Nil || request.Ordinal < 0 || request.Ordinal >= 5000 || request.SourceObservation == uuid.Nil || request.DonorObservation == uuid.Nil || request.SourceObservation == request.DonorObservation {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	shape := v2portrait.OpenEyesParams{SchemaVersion: 1, Base: request.Base, Donor: request.Donor, Strength: request.Strength,
		SourceFaceID: uuid.NameSpaceOID, DonorFaceID: uuid.NameSpaceDNS, EyesMask: request.EyesMask, Protected: request.Protected,
		Confirmation: v2input.Ref{ArtifactID: uuid.NameSpaceX500, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		SourceFaces:  request.SourceFaces, DonorFaces: request.DonorFaces}
	if _, _, err = shape.Compile(); err != nil {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{request.Base.Source, request.Donor.Source, request.EyesMask, request.Protected, request.TargetPixel, request.ProtectedPixel, request.SourceFaces, request.DonorFaces} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if _, err = domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	for _, digest := range []string{request.Base.CanvasHash, request.Donor.CanvasHash} {
		if _, err = domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	return request, nil
}
