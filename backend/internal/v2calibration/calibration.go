package v2calibration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"regexp"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type calibration struct {
	SchemaVersion            int       `json:"schema_version"`
	ModelLockSHA             string    `json:"model_lock_sha"`
	DatasetSHA               string    `json:"dataset_sha"`
	Qualified                *bool     `json:"qualified"`
	SceneCosineMin           *float64  `json:"scene_cosine_min"`
	SceneLogLumaMax          *float64  `json:"scene_log_luma_max"`
	ClusterDiameterMax       *float64  `json:"cluster_diameter_max"`
	SharpnessScale           *float64  `json:"sharpness_scale"`
	ClippingPenalty          *float64  `json:"clipping_penalty"`
	MatchInlierMin           *float64  `json:"match_inlier_min"`
	MatchErrorPxMax          *float64  `json:"match_error_px_max"`
	IdentitySimilarityMin    *float64  `json:"identity_similarity_min"`
	LandmarkDriftMax         *float64  `json:"landmark_drift_max"`
	NormalizationScales      []float64 `json:"normalization_scales"`
	LossWeights              []float64 `json:"loss_weights"`
	FocusWarning             *float64  `json:"focus_warning"`
	FocusCritical            *float64  `json:"focus_critical"`
	EyeOpenMin               *float64  `json:"eye_open_min"`
	GlareWarningFraction     *float64  `json:"glare_warning_fraction"`
	ClippingWarningFraction  *float64  `json:"clipping_warning_fraction"`
	ClippingCriticalFraction *float64  `json:"clipping_critical_fraction"`
	MinFaceEdgePx            int       `json:"min_face_edge_px"`
	DepthSigma               *float64  `json:"depth_sigma"`
	RegistrationErrorPx      *float64  `json:"registration_error_px"`
	MotionThreshold          *float64  `json:"motion_threshold"`
}

func bounded(value *float64, min, max float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= min && *value <= max
}
func Validate(raw []byte, modelLock, datasetLock string) error {
	if len(raw) == 0 || len(raw) > 1_048_576 {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	if _, err := canonical.Bytes(raw); err != nil {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	var c calibration
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || c.SchemaVersion != 1 || c.ModelLockSHA != modelLock ||
		!digestPattern.MatchString(c.DatasetSHA) || c.DatasetSHA != datasetLock ||
		c.Qualified == nil || !*c.Qualified ||
		!bounded(c.SceneCosineMin, -1, 1) || !bounded(c.SceneLogLumaMax, 0, 4) ||
		!bounded(c.ClusterDiameterMax, 0, 2) || !bounded(c.SharpnessScale, 1e-6, 1e6) ||
		!bounded(c.ClippingPenalty, 0, 10) || !bounded(c.MatchInlierMin, 0, 1) ||
		!bounded(c.MatchErrorPxMax, 0, 100) || !bounded(c.IdentitySimilarityMin, -1, 1) ||
		!bounded(c.LandmarkDriftMax, 0, 1) || len(c.NormalizationScales) != 6 ||
		len(c.LossWeights) != 6 || !bounded(c.FocusWarning, 0, 1) ||
		!bounded(c.FocusCritical, 0, 1) || !bounded(c.EyeOpenMin, 0, 1) ||
		!bounded(c.GlareWarningFraction, 0, 1) || !bounded(c.ClippingWarningFraction, 0, 1) ||
		!bounded(c.ClippingCriticalFraction, 0, 1) || c.MinFaceEdgePx < 32 || c.MinFaceEdgePx > 512 ||
		!bounded(c.DepthSigma, .01, 1) || !bounded(c.RegistrationErrorPx, .01, 10) ||
		!bounded(c.MotionThreshold, .001, 1) {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	for _, x := range c.NormalizationScales {
		if !bounded(&x, 1e-6, 1e6) {
			return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
		}
	}
	weightSum := 0.0
	for _, x := range c.LossWeights {
		if !bounded(&x, 0, 1) {
			return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
		}
		weightSum += x
	}
	if math.Abs(weightSum-1) > 1e-9 {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	return nil
}

func Verify(raw []byte, expectedSHA, modelLock, datasetLock string) error {
	if len(raw) == 0 || len(raw) > 1_048_576 || !digestPattern.MatchString(expectedSHA) || !digestPattern.MatchString(modelLock) || !digestPattern.MatchString(datasetLock) {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expectedSHA {
		return domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	return Validate(raw, modelLock, datasetLock)
}

// CalibrationMinFaceEdge verifies the complete pinned document before exposing
// its geometry threshold. It does not establish live artifact/catalog authority.
func CalibrationMinFaceEdge(raw []byte, expectedSHA, modelLock, datasetLock string) (int, error) {
	if !digestPattern.MatchString(expectedSHA) || !digestPattern.MatchString(modelLock) || !digestPattern.MatchString(datasetLock) {
		return 0, domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	if err := Verify(raw, expectedSHA, modelLock, datasetLock); err != nil {
		return 0, err
	}
	var c calibration
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, domain.NewError("CALIBRATION_UNAVAILABLE", 503)
	}
	return c.MinFaceEdgePx, nil
}
