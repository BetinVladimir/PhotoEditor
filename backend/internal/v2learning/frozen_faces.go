package v2learning

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
)

func faceUnit(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) &&
		*value >= 0 && *value <= 1
}

// ValidateFrozenFaceObservations checks the exact source-bound descriptor
// before it enters a learning.evaluate closure. It does not infer a successful
// detector or grant matching consent from an empty document.
func ValidateFrozenFaceObservations(raw []byte,
	binding v2input.SessionVersionBinding) (int, error) {
	if len(raw) < 2 || len(raw) > 1048576 || binding.AssetVersionID == uuid.Nil ||
		!validManifestRef(binding.Source) || !validSHA(binding.CanvasHash) {
		return 0, ErrInvalidTrainingManifest
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return 0, ErrInvalidTrainingManifest
	}
	var evidence v2observation.Evidence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&evidence) != nil || evidence.SchemaVersion != 1 ||
		evidence.Binding.AssetVersionID != binding.AssetVersionID ||
		evidence.Binding.Source.ArtifactID != binding.Source.ArtifactID ||
		evidence.Binding.Source.SHA256 != binding.Source.SHA256 ||
		evidence.Binding.CanvasHash != binding.CanvasHash ||
		evidence.Faces == nil || len(evidence.Faces) > 100 {
		return 0, ErrInvalidTrainingManifest
	}
	encoded, err := canonical.Marshal(evidence)
	if err != nil || !bytes.Equal(encoded, raw) {
		return 0, ErrInvalidTrainingManifest
	}
	observations := make(map[uuid.UUID]struct{}, len(evidence.Faces))
	faces := make(map[uuid.UUID]struct{}, len(evidence.Faces))
	for _, face := range evidence.Faces {
		if face.ObservationID == uuid.Nil || face.FaceID == uuid.Nil ||
			face.Quality == nil || !faceUnit(face.Quality) || face.Box == nil ||
			!faceUnit(face.Box.X0) || !faceUnit(face.Box.Y0) ||
			!faceUnit(face.Box.X1) || !faceUnit(face.Box.Y1) ||
			*face.Box.X0 >= *face.Box.X1 || *face.Box.Y0 >= *face.Box.Y1 ||
			len(face.Landmarks) != 68 ||
			(face.PersonID == nil) != (face.Embedding == nil) {
			return 0, ErrInvalidTrainingManifest
		}
		if _, duplicate := observations[face.ObservationID]; duplicate {
			return 0, ErrInvalidTrainingManifest
		}
		if _, duplicate := faces[face.FaceID]; duplicate {
			return 0, ErrInvalidTrainingManifest
		}
		if face.PersonID != nil && (*face.PersonID == uuid.Nil ||
			len(face.Embedding) != 512) {
			return 0, ErrInvalidTrainingManifest
		}
		for _, point := range face.Landmarks {
			if !faceUnit(point.X) || !faceUnit(point.Y) {
				return 0, ErrInvalidTrainingManifest
			}
		}
		for _, value := range face.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) ||
				value < -1 || value > 1 {
				return 0, ErrInvalidTrainingManifest
			}
		}
		observations[face.ObservationID] = struct{}{}
		faces[face.FaceID] = struct{}{}
	}
	return len(evidence.Faces), nil
}
