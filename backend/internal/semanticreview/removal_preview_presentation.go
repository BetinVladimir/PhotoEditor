package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/canonical"
)

// RemovalPreviewPresentation is display linkage only. Canonical producer and
// all four physical PNG versions must be checked independently before URLs.
type RemovalPreviewPresentation struct {
	SchemaVersion     int                  `json:"schema_version"`
	Params            RemovalPreviewParams `json:"params"`
	NumericalEvidence v2input.Ref          `json:"numerical_evidence"`
	WorkingImage      v2input.Ref          `json:"working_image"`
	Pixels            v2input.Ref          `json:"pixels"`
	Before            v2input.Ref          `json:"before"`
	After             v2input.Ref          `json:"after"`
	Affected          v2input.Ref          `json:"affected"`
	Protected         v2input.Ref          `json:"protected"`
	Width             int                  `json:"width"`
	Height            int                  `json:"height"`
	ICCLockSHA        string               `json:"icc_lock_sha"`
	SRGBSHA           string               `json:"srgb_sha"`
	Resize            string               `json:"resize"`
	Overlay           string               `json:"overlay"`
}

func DecodeRemovalPreviewPresentation(ref v2input.Ref, raw []byte, expected VerifiedRemovalPreviewEvidence,
	iccLockSHA, srgbSHA string) (RemovalPreviewPresentation, error) {
	empty := RemovalPreviewPresentation{}
	if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || len(raw) < 2 || len(raw) > 1048576 ||
		!shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
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
	for _, key := range []string{"schema_version", "params", "numerical_evidence", "working_image", "pixels", "before", "after", "affected", "protected", "width", "height", "icc_lock_sha", "srgb_sha", "resize", "overlay"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return empty, ErrInvalid
		}
	}
	var p RemovalPreviewPresentation
	if decodeStrict(raw, &p) != nil || p.SchemaVersion != 1 || p.Width < 1 || p.Width > 2560 || p.Height < 1 || p.Height > 2560 ||
		p.ICCLockSHA != iccLockSHA || p.SRGBSHA != srgbSHA || p.Resize != "area_linear_premult_v1" || p.Overlay != "max_coverage_ceil_alpha_v1" ||
		p.NumericalEvidence != expected.Source.Ref || p.WorkingImage != expected.Evidence.WorkingImage || p.Pixels != expected.Evidence.Pixels {
		return empty, ErrInvalid
	}
	_, expectedSHA, err := FreezeRemovalPreviewParams(expected.Evidence.Params)
	if err != nil {
		return empty, err
	}
	_, actualSHA, err := FreezeRemovalPreviewParams(p.Params)
	if err != nil || actualSHA != expectedSHA {
		return empty, ErrInvalid
	}
	refs := []v2input.Ref{ref, p.Params.Plan, p.Params.Intent, p.Params.Candidate, p.Params.Source.Source, p.Params.Protection,
		p.NumericalEvidence, p.WorkingImage, p.Pixels, p.Before, p.After, p.Affected, p.Protected}
	seen := map[uuid.UUID]bool{}
	for _, value := range refs {
		if value.ArtifactID == uuid.Nil || seen[value.ArtifactID] || !shaPattern.MatchString(value.SHA256) {
			return empty, ErrInvalid
		}
		seen[value.ArtifactID] = true
	}
	body, err := canonical.Marshal(p)
	if err != nil || !bytes.Equal(body, raw) {
		return empty, ErrInvalid
	}
	return p, nil
}
