package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ValidateReviewedDefectStyle binds the accepted training base style to the
// effective style used by the decided repair task. Both byte strings must
// already come from current exact-version, project-scoped object reads.
func ValidateReviewedDefectStyle(styleRaw, resolvedRaw []byte,
	styleRef, resolvedRef, imageRef v2input.Ref) error {
	if !validManifestRef(styleRef) || !validManifestRef(resolvedRef) ||
		!validManifestRef(imageRef) || styleRef.ArtifactID == resolvedRef.ArtifactID ||
		styleRef.ArtifactID == imageRef.ArtifactID ||
		resolvedRef.ArtifactID == imageRef.ArtifactID {
		return ErrInvalidTrainingDraft
	}
	decode := func(raw []byte, ref v2input.Ref) (map[string]json.RawMessage, error) {
		if len(raw) < 2 || len(raw) > 1<<20 {
			return nil, ErrInvalidTrainingDraft
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != ref.SHA256 {
			return nil, ErrInvalidTrainingDraft
		}
		canonicalRaw, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(canonicalRaw, raw) {
			return nil, ErrInvalidTrainingDraft
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
			return nil, ErrInvalidTrainingDraft
		}
		roundtrip, err := canonical.Marshal(fields)
		if err != nil || !bytes.Equal(roundtrip, raw) {
			return nil, ErrInvalidTrainingDraft
		}
		return fields, nil
	}
	style, err := decode(styleRaw, styleRef)
	if err != nil {
		return err
	}
	resolved, err := decode(resolvedRaw, resolvedRef)
	if err != nil {
		return err
	}
	var styleVersion, renderer string
	var styleStrength float64
	if json.Unmarshal(style["schema_version"], &styleVersion) != nil ||
		json.Unmarshal(style["renderer_version"], &renderer) != nil ||
		json.Unmarshal(style["strength"], &styleStrength) != nil ||
		styleVersion != "2.0.0" || renderer != "photo_renderer_v2" ||
		styleStrength <= 0 || styleStrength > 1 {
		return ErrInvalidTrainingDraft
	}
	var defects struct {
		Enabled                bool      `json:"enabled"`
		Algorithm              string    `json:"algorithm"`
		Policy                 string    `json:"policy"`
		Strength               float64   `json:"strength"`
		DiameterFaceFraction   []float64 `json:"diameter_face_fraction"`
		PreservePermanentMarks bool      `json:"preserve_permanent_marks"`
	}
	if json.Unmarshal(style["defects"], &defects) != nil ||
		!defects.Enabled || defects.Algorithm != "defectrepair_telea_v1" ||
		defects.Policy != "review" || defects.Strength <= 0 ||
		defects.Strength > 1 || !defects.PreservePermanentMarks ||
		len(defects.DiameterFaceFraction) != 2 ||
		defects.DiameterFaceFraction[0] < .002 ||
		defects.DiameterFaceFraction[0] > defects.DiameterFaceFraction[1] ||
		defects.DiameterFaceFraction[1] > .012 {
		return ErrInvalidTrainingDraft
	}
	var resolvedHash, mode string
	var resolvedStrength float64
	if json.Unmarshal(resolved["hash"], &resolvedHash) != nil ||
		json.Unmarshal(resolved["mode"], &mode) != nil ||
		json.Unmarshal(resolved["strength"], &resolvedStrength) != nil ||
		(mode != "fixed" && mode != "adaptive" && mode != "fixed_fallback" &&
			mode != "group_fixed") || resolvedStrength <= 0 ||
		resolvedStrength > 1 || !validSHA(resolvedHash) {
		return ErrInvalidTrainingDraft
	}
	delete(resolved, "hash")
	computed, err := canonical.Hash(resolved)
	if err != nil || computed != resolvedHash {
		return ErrInvalidTrainingDraft
	}
	var params struct {
		Style *v2input.Ref `json:"style"`
		Image v2input.Ref  `json:"image"`
	}
	if json.Unmarshal(resolved["params"], &params) != nil ||
		params.Style == nil || *params.Style != styleRef ||
		params.Image != imageRef {
		return ErrInvalidTrainingDraft
	}
	var modules map[string]json.RawMessage
	if json.Unmarshal(resolved["modules"], &modules) != nil ||
		!bytes.Equal(modules["defects"], style["defects"]) {
		return ErrInvalidTrainingDraft
	}
	return nil
}
