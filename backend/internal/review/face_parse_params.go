package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"photoeditor/backend/internal/canonical"
)

type FaceParseParams struct {
	Image           ArtifactRef `json:"image"`
	Faces           ArtifactRef `json:"faces"`
	ModelID         string      `json:"model_id"`
	ModelLockID     string      `json:"model_lock_id"`
	PreprocessingID string      `json:"preprocessing_id"`
	CropSize        int         `json:"crop_size"`
	Expansion       float64     `json:"expansion"`
}

// VerifyFaceParseParams binds the original producer configuration and complete
// original detector inventory to its parsed manifest. Producer catalog, model
// qualification, actual inference and uint16 labels remain caller obligations.
func VerifyFaceParseParams(paramsRaw []byte, paramsRef ArtifactRef, labelsRaw []byte, labelsRef ArtifactRef, detectionRaw []byte, detectionRef ArtifactRef, source DefectSource) (FaceLabels19, error) {
	fail := func() (FaceLabels19, error) {
		return FaceLabels19{}, errors.New("invalid face parse producer evidence")
	}
	for _, pin := range []struct {
		raw []byte
		ref ArtifactRef
	}{{paramsRaw, paramsRef}, {labelsRaw, labelsRef}, {detectionRaw, detectionRef}} {
		if !pin.ref.valid() || len(pin.raw) < 2 || len(pin.raw) > 1048576 {
			return fail()
		}
		sum := sha256.Sum256(pin.raw)
		if hex.EncodeToString(sum[:]) != pin.ref.SHA256 {
			return fail()
		}
	}
	if paramsRef.ID == labelsRef.ID || paramsRef.ID == detectionRef.ID || labelsRef.ID == detectionRef.ID {
		return fail()
	}
	labels, err := ParseFaceLabels19(labelsRaw, source)
	if err != nil {
		return fail()
	}
	detection, err := ParseFaceBoxes(detectionRaw, source)
	if err != nil {
		return fail()
	}
	var params FaceParseParams
	if decodeExact(paramsRaw, &params) != nil || params.Image != source.Image || params.Faces != detectionRef || labels.Faces != detectionRef || labels.ParsedFaces != len(detection.Faces) || params.ModelID != labels.ModelID || params.ModelLockID != labels.ModelLockID || params.PreprocessingID != labels.PreprocessingID || params.CropSize != 512 || params.Expansion != 1.35 {
		return fail()
	}
	return labels, nil
}

// VerifyFaceSkinConfidence resolves the empty/nonempty confidence branch after
// VerifyFaceSkinNPY has proved actual class1 derivation. Count alone is not evidence
// and must never come from user input. This does not authorize producer/admission.
func VerifyFaceSkinConfidence(mask ProtectionMask, labels FaceLabels19, verifiedSkinPixels int64) error {
	if verifiedSkinPixels < 0 || !validCanvas(labels.Canvas) || verifiedSkinPixels > int64(labels.Canvas.Width)*int64(labels.Canvas.Height) || !validMaskBinding(mask) || mask.Canvas != labels.Canvas || mask.Selector != "skin" || mask.Protected || mask.ObjectID != nil || mask.ModelLockID == nil || *mask.ModelLockID != labels.ModelLockID {
		return errors.New("invalid skin confidence evidence")
	}
	if math.IsNaN(labels.MeanConfidence) || math.IsInf(labels.MeanConfidence, 0) || labels.MeanConfidence < 0 || labels.MeanConfidence > 1 || labels.ParsedFaces < 0 || labels.ParsedFaces > 100 || (labels.ParsedFaces == 0 && (labels.MeanConfidence != 0 || verifiedSkinPixels != 0)) {
		return errors.New("invalid parse confidence metadata")
	}
	a, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return err
	}
	b, err := canonical.Bytes(labels.TransformChain)
	if err != nil || !bytes.Equal(a, b) {
		return errors.New("skin confidence geometry mismatch")
	}
	expected := 0.0
	if verifiedSkinPixels > 0 {
		expected = labels.MeanConfidence
	}
	if mask.Confidence != expected {
		return errors.New("skin confidence does not match physical labels")
	}
	return nil
}

// Internal alias preserves existing verifier fixtures.
type faceParseParams = FaceParseParams
