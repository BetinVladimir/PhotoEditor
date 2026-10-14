package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// OpenEyesParams describes explicit donor replacement. Compilation checks wire
// shape only; same-person/series, consent, face geometry, masks and current
// artifact authority must be independently established before execution.
type OpenEyesParams struct {
	SchemaVersion int                           `json:"schema_version"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Strength      float64                       `json:"strength"`
	Donor         v2input.SessionVersionBinding `json:"donor"`
	SourceFaceID  uuid.UUID                     `json:"source_face_id"`
	DonorFaceID   uuid.UUID                     `json:"donor_face_id"`
	EyesMask      v2input.Ref                   `json:"eyes_mask"`
	Protected     v2input.Ref                   `json:"protected"`
	Confirmation  v2input.Ref                   `json:"confirmation"`
	SourceFaces   v2input.Ref                   `json:"source_faces"`
	DonorFaces    v2input.Ref                   `json:"donor_faces"`
}

func (p OpenEyesParams) Compile() ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if !validLocalBase(p.SchemaVersion, p.Base, p.EyesMask, p.Protected, p.Strength) ||
		!validLocalBase(p.SchemaVersion, p.Donor, p.EyesMask, p.Protected, p.Strength) ||
		p.SourceFaceID == uuid.Nil || p.DonorFaceID == uuid.Nil || p.Base.AssetVersionID == p.Donor.AssetVersionID ||
		p.Base.Source.ArtifactID == p.Donor.Source.ArtifactID || p.EyesMask.ArtifactID == p.Protected.ArtifactID {
		return invalid()
	}
	for _, ref := range []v2input.Ref{p.Confirmation, p.SourceFaces, p.DonorFaces} {
		if ref.ArtifactID == uuid.Nil {
			return invalid()
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return invalid()
		}
	}
	return compileParams(p)
}
