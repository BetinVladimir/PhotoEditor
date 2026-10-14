package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type FaceLabels19 struct {
	Image           ArtifactRef     `json:"image"`
	Faces           ArtifactRef     `json:"faces"`
	Labels          ArtifactRef     `json:"labels"`
	Canvas          Canvas          `json:"canvas"`
	TransformChain  json.RawMessage `json:"transform_chain"`
	Classes         int             `json:"classes"`
	ModelID         string          `json:"model_id"`
	ModelLockID     string          `json:"model_lock_id"`
	Backend         string          `json:"backend"`
	PreprocessingID string          `json:"preprocessing_id"`
	ParsedFaces     int             `json:"parsed_faces"`
	MeanConfidence  float64         `json:"mean_confidence"`
	Warnings        []string        `json:"warnings"`
}

// ParseFaceLabels19 checks source-bound metadata; actual labels, face inventory,
// canonical producer authority and scientific qualification remain separate.
func ParseFaceLabels19(raw []byte, source DefectSource) (FaceLabels19, error) {
	fail := func() (FaceLabels19, error) { return FaceLabels19{}, errors.New("invalid FaceLabels19") }
	var d FaceLabels19
	if decodeExact(raw, &d) != nil || d.Image != source.Image || !d.Image.valid() || !d.Faces.valid() || !d.Labels.valid() || d.Canvas != source.Canvas || !validCanvas(d.Canvas) || d.Classes != 19 || d.ModelID != "bisenet_face19" || d.PreprocessingID != "bisenet_face19_crop512_v1" || d.ParsedFaces < 0 || d.ParsedFaces > 100 || d.Warnings == nil || len(d.Warnings) > 4 || math.IsNaN(d.MeanConfidence) || math.IsInf(d.MeanConfidence, 0) || d.MeanConfidence < 0 || d.MeanConfidence > 1 {
		return fail()
	}
	if _, err := domain.ParseDigest(d.ModelLockID); err != nil {
		return fail()
	}
	if d.Backend != "cpu" && d.Backend != "cuda" && d.Backend != "mps" {
		return fail()
	}
	if validateTransformChain(d.TransformChain, d.Canvas) != nil {
		return fail()
	}
	a, err := canonical.Bytes(d.TransformChain)
	if err != nil {
		return fail()
	}
	b, err := canonical.Bytes(source.TransformChain)
	if err != nil || !bytes.Equal(a, b) {
		return fail()
	}
	seen := map[string]bool{}
	for _, warning := range d.Warnings {
		if seen[warning] {
			return fail()
		}
		seen[warning] = true
		switch warning {
		case "no_faces", "low_confidence", "overlapping_faces", "source_srgb_clipped":
		default:
			return fail()
		}
	}
	if seen["no_faces"] != (d.ParsedFaces == 0) || seen["low_confidence"] != (d.ParsedFaces > 0 && d.MeanConfidence < .75) || (d.ParsedFaces == 0 && (d.MeanConfidence != 0 || seen["overlapping_faces"])) {
		return fail()
	}
	return d, nil
}

// VerifyFaceSkinParams binds original Params, original parse manifest and mask.
// Skin confidence is zero for empty skin and parse mean otherwise; pixel checks
// must determine which branch applies before admission.
func VerifyFaceSkinParams(paramsRaw []byte, paramsRef ArtifactRef, labelsRaw []byte, labelsRef ArtifactRef, mask ProtectionMask, source DefectSource) (FaceLabels19, error) {
	fail := func() (FaceLabels19, error) { return FaceLabels19{}, errors.New("invalid face skin Params") }
	var params struct {
		Labels    ArtifactRef `json:"labels"`
		Algorithm string      `json:"algorithm"`
	}
	for _, pin := range []struct {
		raw []byte
		ref ArtifactRef
	}{{paramsRaw, paramsRef}, {labelsRaw, labelsRef}} {
		if !pin.ref.valid() || len(pin.raw) < 2 || len(pin.raw) > 1048576 {
			return fail()
		}
		sum := sha256.Sum256(pin.raw)
		if hex.EncodeToString(sum[:]) != pin.ref.SHA256 {
			return fail()
		}
	}
	if decodeExact(paramsRaw, &params) != nil || params.Algorithm != "bisenet_face19_skin_v1" || params.Labels != labelsRef {
		return fail()
	}
	labels, err := ParseFaceLabels19(labelsRaw, source)
	if err != nil {
		return fail()
	}
	if !validMaskBinding(mask) || mask.Selector != "skin" || mask.Protected || mask.ObjectID != nil || mask.ModelLockID == nil || *mask.ModelLockID != labels.ModelLockID || mask.Canvas != labels.Canvas || (mask.Confidence != 0 && mask.Confidence != labels.MeanConfidence) {
		return fail()
	}
	a, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return fail()
	}
	b, _ := canonical.Bytes(labels.TransformChain)
	if !bytes.Equal(a, b) {
		return fail()
	}
	return labels, nil
}
