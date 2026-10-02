package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ValidateFrozenEvaluateConsent rejects a snapshot that is not the exact
// canonical, structurally valid document. A shared snapshot may contain
// confirmed faces from other selected source versions. Current grant state
// must still be checked at evaluation adoption, independently of this body.
func ValidateFrozenEvaluateConsent(raw, faceRaw []byte,
	binding v2input.SessionVersionBinding) error {
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) ||
		v2input.ValidateSessionConsentSnapshot(raw) != nil ||
		binding.AssetVersionID == uuid.Nil {
		return ErrInvalidTrainingDraft
	}
	var snapshot struct {
		ConfirmedFaces []struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
			FaceID         uuid.UUID `json:"face_id"`
			PersonID       uuid.UUID `json:"person_id"`
		} `json:"confirmed_faces"`
	}
	var observations struct {
		Faces []struct {
			FaceID    uuid.UUID  `json:"face_id"`
			PersonID  *uuid.UUID `json:"person_id"`
			Embedding []float64  `json:"embedding"`
		} `json:"faces"`
	}
	if json.Unmarshal(raw, &snapshot) != nil ||
		json.Unmarshal(faceRaw, &observations) != nil || observations.Faces == nil {
		return ErrInvalidTrainingDraft
	}
	confirmed := make(map[uuid.UUID]uuid.UUID)
	for _, face := range snapshot.ConfirmedFaces {
		if face.AssetVersionID == binding.AssetVersionID {
			confirmed[face.FaceID] = face.PersonID
		}
	}
	for _, face := range observations.Faces {
		person, okay := confirmed[face.FaceID]
		if !okay || face.PersonID == nil || *face.PersonID != person ||
			len(face.Embedding) != 512 {
			return ErrInvalidTrainingDraft
		}
	}
	return nil
}
