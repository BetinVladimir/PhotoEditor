package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ExposurePreviewEvidence is numerical replay evidence only. A provider must
// independently prove canonical producer attempt/fence, physical objects,
// image/pixel binding and presentation provenance before exposing a Preview.
type ExposurePreviewEvidence struct {
	SchemaVersion            int                   `json:"schema_version"`
	Params                   ExposurePreviewParams `json:"params"`
	WorkingImage             v2input.Ref           `json:"working_image"`
	Pixels                   v2input.Ref           `json:"pixels"`
	Algorithm                string                `json:"algorithm"`
	ProtectedPixelsUnchanged bool                  `json:"protected_pixels_unchanged"`
	OutsideMaskMaxError      float64               `json:"outside_mask_max_error"`
}

func DecodeExposurePreviewEvidence(ref v2input.Ref, raw []byte, expected ExposurePreviewParams) (ExposurePreviewEvidence, error) {
	empty := ExposurePreviewEvidence{}
	if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || len(raw) < 2 || len(raw) > 1048576 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return empty, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return empty, ErrInvalid
	}
	for _, key := range []string{"schema_version", "params", "working_image", "pixels", "algorithm", "protected_pixels_unchanged", "outside_mask_max_error"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return empty, ErrInvalid
		}
	}
	var evidence ExposurePreviewEvidence
	if decodeStrict(raw, &evidence) != nil || evidence.SchemaVersion != 1 || evidence.Algorithm != "local_exposure_exact_replay_v1" ||
		!evidence.ProtectedPixelsUnchanged || evidence.OutsideMaskMaxError != 0 {
		return empty, ErrInvalid
	}
	_, expectedSHA, err := FreezeExposurePreviewParams(expected)
	if err != nil {
		return empty, err
	}
	_, actualSHA, err := FreezeExposurePreviewParams(evidence.Params)
	if err != nil || actualSHA != expectedSHA {
		return empty, ErrInvalid
	}
	refs := []v2input.Ref{ref, expected.Plan, expected.Intent, expected.Candidate, expected.Source.Source, expected.Protection, evidence.WorkingImage, evidence.Pixels}
	for i, item := range refs {
		if item.ArtifactID == uuid.Nil || !shaPattern.MatchString(item.SHA256) {
			return empty, ErrInvalid
		}
		for j := 0; j < i; j++ {
			if refs[j].ArtifactID == item.ArtifactID {
				return empty, ErrInvalid
			}
		}
	}
	return evidence, nil
}
