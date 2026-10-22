package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RetouchPreviewEvidence is numerical replay evidence only. A provider must
// independently prove canonical producer attempt/fence, physical objects,
// image/pixel binding and presentation provenance before exposing a Preview.
type RetouchPreviewEvidence struct {
	SchemaVersion            int                  `json:"schema_version"`
	Params                   RetouchPreviewParams `json:"params"`
	WorkingImage             v2input.Ref          `json:"working_image"`
	Pixels                   v2input.Ref          `json:"pixels"`
	Algorithm                string               `json:"algorithm"`
	ProtectedPixelsUnchanged bool                 `json:"protected_pixels_unchanged"`
	OutsideMaskUnchanged     bool                 `json:"outside_mask_unchanged"`
	ChangedPixels            int64                `json:"changed_pixels"`
	AlphaUnchanged           bool                 `json:"alpha_unchanged"`
	ROI                      []int64              `json:"roi"`
}

func DecodeRetouchPreviewEvidence(ref v2input.Ref, raw []byte, expected RetouchPreviewParams) (RetouchPreviewEvidence, error) {
	empty := RetouchPreviewEvidence{}
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
	for _, key := range []string{"schema_version", "params", "working_image", "pixels", "algorithm", "protected_pixels_unchanged", "outside_mask_unchanged", "changed_pixels", "alpha_unchanged", "roi"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return empty, ErrInvalid
		}
	}
	var evidence RetouchPreviewEvidence
	if decodeStrictWithLimit(raw, &evidence, 1048576) != nil || evidence.SchemaVersion != 1 || evidence.Algorithm != "beautyfilter_v1" ||
		!evidence.ProtectedPixelsUnchanged || !evidence.OutsideMaskUnchanged || !evidence.AlphaUnchanged || evidence.ChangedPixels < 0 || evidence.ChangedPixels > 200000000 || len(evidence.ROI) != 4 {
		return empty, ErrInvalid
	}
	// Bound arithmetic before multiplying ROI dimensions; safe JSON integers
	// match the platform contract and cannot overflow the area calculation.
	for _, coordinate := range evidence.ROI {
		if coordinate < 0 || coordinate > 200000000 {
			return empty, ErrInvalid
		}
	}
	x0, y0, x1, y1 := evidence.ROI[0], evidence.ROI[1], evidence.ROI[2], evidence.ROI[3]
	if x1 <= x0 || y1 <= y0 {
		return empty, ErrInvalid
	}
	width, height := x1-x0, y1-y0
	// Compare using division to avoid overflow even for hostile coordinates.
	if evidence.ChangedPixels > 0 && (evidence.ChangedPixels-1)/width >= height {
		return empty, ErrInvalid
	}
	_, expectedSHA, err := FreezeRetouchPreviewParams(expected)
	if err != nil {
		return empty, err
	}
	_, actualSHA, err := FreezeRetouchPreviewParams(evidence.Params)
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
	body, err := canonical.Marshal(evidence)
	if err != nil || !bytes.Equal(body, raw) {
		return empty, ErrInvalid
	}
	return evidence, nil
}
