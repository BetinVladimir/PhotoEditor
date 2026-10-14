package v2input

import (
	"bytes"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type semanticPersonParams struct {
	Image         Ref    `json:"image"`
	Model         string `json:"model_id"`
	Lock          string `json:"model_lock_id"`
	Preprocessing string `json:"preprocessing_id"`
	LongSide      int    `json:"long_side"`
	GuidedTile    int    `json:"guided_tile"`
}
type semanticPersonReport struct {
	Image         Ref      `json:"image"`
	Mask          Ref      `json:"mask"`
	Width         int      `json:"preview_width"`
	Height        int      `json:"preview_height"`
	Model         string   `json:"model_id"`
	Lock          string   `json:"model_lock_id"`
	Backend       string   `json:"backend"`
	Preprocessing string   `json:"preprocessing_id"`
	Class         int      `json:"person_class"`
	Confidence    float64  `json:"confidence"`
	Fraction      float64  `json:"person_fraction"`
	Clip          float64  `json:"source_clip_fraction"`
	Alpha         float64  `json:"alpha_excluded_fraction"`
	Warnings      []string `json:"warnings"`
}

func decodePersonEvidence(raw []byte, value any) error {
	if len(raw) < 2 || len(raw) > 1048576 {
		return ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return ErrInvalid
	}
	encoded, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(encoded, raw) {
		return ErrInvalid
	}
	return nil
}

// VerifySemanticPersonEvidence binds original canonical worker Params/report
// bytes to exact source/mask/model/backend and deterministic preprocessing.
// Catalog SHA/version, canonical producer and scientific qualification are
// independent caller obligations; numeric report claims are not measured here.
func VerifySemanticPersonEvidence(binding SessionVersionBinding, image json.RawMessage, root Ref, mask json.RawMessage, paramsRaw, reportRaw []byte, modelLock, backend string) error {
	if backend != "cpu" && backend != "cuda" && backend != "mps" {
		return ErrInvalid
	}
	pixels, err := ResolveSemanticPersonMaskPixels(root, mask, binding, image, modelLock)
	if err != nil {
		return err
	}
	metadata, err := ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return err
	}
	var params semanticPersonParams
	var report semanticPersonReport
	if decodePersonEvidence(paramsRaw, &params) != nil || decodePersonEvidence(reportRaw, &report) != nil {
		return ErrInvalid
	}
	if params.Image != binding.Source || params.Model != "lraspp_mobilenet_v3_large" || params.Lock != modelLock || params.Preprocessing != "person_guided_v1" || params.LongSide != 768 || (params.GuidedTile != 256 && params.GuidedTile != 512 && params.GuidedTile != 1024) {
		return ErrInvalid
	}
	scale := 768 / math.Max(float64(metadata.Width), float64(metadata.Height))
	width := int(math.Max(1, math.RoundToEven(float64(metadata.Width)*scale)))
	height := int(math.Max(1, math.RoundToEven(float64(metadata.Height)*scale)))
	if report.Image != binding.Source || report.Mask != pixels || report.Model != params.Model || report.Lock != modelLock || report.Backend != backend || report.Preprocessing != params.Preprocessing || report.Class != 15 || report.Width != width || report.Height != height {
		return ErrInvalid
	}
	for _, value := range []float64{report.Confidence, report.Fraction, report.Clip, report.Alpha} {
		if !(value >= 0 && value <= 1) {
			return ErrInvalid
		}
	}
	parsed, err := review.ParseMaskRef(mask)
	if err != nil || parsed.Confidence != report.Confidence {
		return ErrInvalid
	}
	expected := make([]string, 0, 3)
	if report.Confidence < .75 {
		expected = append(expected, "low_confidence")
	}
	if report.Fraction < .01 {
		expected = append(expected, "empty_person")
	}
	if report.Clip > 0 {
		expected = append(expected, "source_srgb_clipped")
	}
	if report.Warnings == nil || len(report.Warnings) != len(expected) {
		return ErrInvalid
	}
	for i, value := range expected {
		if report.Warnings[i] != value {
			return ErrInvalid
		}
	}
	return nil
}
