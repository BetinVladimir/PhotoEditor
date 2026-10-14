package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type personEvidencePin struct {
	Contract   string    `json:"contract"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	Version    string    `json:"version"`
	Bytes      int64     `json:"bytes"`
	Backend    string    `json:"backend"`
}

// Pins are the exact objects whose original bytes passed evidence validation.
func marshalPersonEvidencePins(pins []personEvidenceObject, bucket string) ([]byte, error) {
	if len(pins) != 2 || bucket == "" || pins[0].contract != "PersonParams" || pins[1].contract != "PersonReport" {
		return nil, v2input.ErrInvalid
	}
	result := make([]personEvidencePin, 2)
	for i, pin := range pins {
		result[i] = personEvidencePin{pin.contract, pin.ref.ArtifactID, pin.ref.SHA256, bucket, pin.object.Key, pin.object.VersionID, pin.object.Bytes, pin.backend}
	}
	return canonical.Marshal(result)
}
