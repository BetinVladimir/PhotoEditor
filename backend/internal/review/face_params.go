package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type FaceDetectParams struct {
	Image           ArtifactRef `json:"image"`
	ModelID         string      `json:"model_id"`
	ModelLockID     string      `json:"model_lock_id"`
	PreprocessingID string      `json:"preprocessing_id"`
	LongSide        int         `json:"long_side"`
	ScoreThreshold  float64     `json:"score_threshold"`
	NMSIOU          float64     `json:"nms_iou"`
	MaxFaces        int         `json:"max_faces"`
}
type LandmarkParams struct {
	Image           ArtifactRef `json:"image"`
	Faces           ArtifactRef `json:"faces"`
	ModelID         string      `json:"model_id"`
	ModelLockID     string      `json:"model_lock_id"`
	PreprocessingID string      `json:"preprocessing_id"`
	CropSize        int         `json:"crop_size"`
	Expansion       float64     `json:"expansion"`
}

func originalFaceParams(raw []byte, ref ArtifactRef) bool {
	if !ref.valid() || len(raw) < 2 || len(raw) > 1048576 {
		return false
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]) == ref.SHA256
}

// VerifyFaceDetectParams checks original Params/output consistency; it does not
// prove that the artifact is the producer's catalog Params or qualify inference.
func VerifyFaceDetectParams(paramsRef ArtifactRef, paramsRaw, boxesRaw []byte, source DefectSource) error {
	invalid := errors.New("invalid detector Params binding")
	if !originalFaceParams(paramsRaw, paramsRef) {
		return invalid
	}
	var p FaceDetectParams
	if decodeExact(paramsRaw, &p) != nil {
		return invalid
	}
	boxes, err := ParseFaceBoxes(boxesRaw, source)
	if err != nil {
		return err
	}
	if p.Image != boxes.Image || p.ModelID != boxes.ModelID || p.ModelLockID != boxes.ModelLockID || p.PreprocessingID != boxes.PreprocessingID || p.LongSide != 1024 || !unitLandmark(p.ScoreThreshold) || p.ScoreThreshold < .5 || p.ScoreThreshold > .99 || !unitLandmark(p.NMSIOU) || p.NMSIOU <= 0 || p.NMSIOU > .5 || p.MaxFaces < 1 || p.MaxFaces > 100 || len(boxes.Faces) > p.MaxFaces {
		return invalid
	}
	for _, box := range boxes.Faces {
		if box.Confidence < p.ScoreThreshold {
			return invalid
		}
	}
	return nil
}

// VerifyLandmarkParams binds original Params to the full exact-source collection.
// Canonical Params ownership and trusted model/runtime remain caller obligations.
func VerifyLandmarkParams(paramsRef ArtifactRef, paramsRaw, landmarkRaw []byte, source DefectSource) error {
	invalid := errors.New("invalid landmark Params binding")
	if !originalFaceParams(paramsRaw, paramsRef) {
		return invalid
	}
	var p LandmarkParams
	if decodeExact(paramsRaw, &p) != nil {
		return invalid
	}
	doc, err := parseFaceLandmarks(landmarkRaw, source)
	if err != nil {
		return err
	}
	if p.Image != doc.Image || p.Faces != doc.Faces || p.ModelID != doc.ModelID || p.ModelLockID != doc.ModelLockID || p.PreprocessingID != doc.PreprocessingID || p.CropSize != 256 || p.Expansion != 1.25 {
		return invalid
	}
	return nil
}
