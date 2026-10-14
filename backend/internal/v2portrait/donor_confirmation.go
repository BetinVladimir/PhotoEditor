package v2portrait

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// DonorConfirmation is authored from confirmed observations and current consent
// in a separate trusted transaction. Compilation alone cannot issue authority.
type DonorConfirmation struct {
	SchemaVersion       int                            `json:"schema_version"`
	PersonID            uuid.UUID                      `json:"person_id"`
	Source              v2input.SessionVersionBinding  `json:"source"`
	Donor               *v2input.SessionVersionBinding `json:"donor"`
	SourceFaceID        uuid.UUID                      `json:"source_face_id"`
	DonorFaceID         *uuid.UUID                     `json:"donor_face_id"`
	SourceObservationID uuid.UUID                      `json:"source_observation_id"`
	DonorObservationID  *uuid.UUID                     `json:"donor_observation_id"`
	ConsentID           uuid.UUID                      `json:"consent_id"`
	Epoch               string                         `json:"epoch"`
	ActorID             uuid.UUID                      `json:"actor_id"`
}

func validDonorBinding(b v2input.SessionVersionBinding) bool {
	if b.AssetVersionID == uuid.Nil || b.Source.ArtifactID == uuid.Nil {
		return false
	}
	if _, err := domain.ParseDigest(b.Source.SHA256); err != nil {
		return false
	}
	_, err := domain.ParseDigest(b.CanvasHash)
	return err == nil
}

func (p DonorConfirmation) Compile() ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if p.SchemaVersion != 1 || p.PersonID == uuid.Nil || p.SourceFaceID == uuid.Nil || p.SourceObservationID == uuid.Nil || p.ConsentID == uuid.Nil || p.ActorID == uuid.Nil || !validDonorBinding(p.Source) || len(p.Epoch) < 1 || len(p.Epoch) > 19 || p.Epoch[0] == '0' {
		return invalid()
	}
	for _, c := range p.Epoch {
		if c < '0' || c > '9' {
			return invalid()
		}
	}
	if p.Donor == nil {
		if p.DonorFaceID != nil || p.DonorObservationID != nil {
			return invalid()
		}
	} else if p.DonorFaceID == nil || p.DonorObservationID == nil || *p.DonorFaceID == uuid.Nil || *p.DonorObservationID == uuid.Nil || !validDonorBinding(*p.Donor) || p.Donor.AssetVersionID == p.Source.AssetVersionID {
		return invalid()
	}
	return compileParams(p)
}

// VerifyOpenEyesConfirmation verifies content/binding, not live DB consent.
func VerifyOpenEyesConfirmation(params OpenEyesParams, raw []byte) (DonorConfirmation, error) {
	var zero DonorConfirmation
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != params.Confirmation.SHA256 {
		return zero, v2input.ErrInvalid
	}
	var proof DonorConfirmation
	if json.Unmarshal(raw, &proof) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, _, err := proof.Compile()
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	// A complete canonical typed roundtrip also refuses unknown/duplicate fields.
	if _, err = canonical.Bytes(raw); err != nil {
		return zero, v2input.ErrInvalid
	}
	if proof.Source != params.Base || proof.Donor == nil || *proof.Donor != params.Donor || proof.SourceFaceID != params.SourceFaceID || proof.DonorFaceID == nil || *proof.DonorFaceID != params.DonorFaceID {
		return zero, v2input.ErrInvalid
	}
	return proof, nil
}
