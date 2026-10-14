package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// VerifySemanticCalibration validates the complete Calibration contract and
// pins against deployment-selected model/dataset values. It is not itself
// proof of qualification: the caller must resolve trusted policy/report first.
func VerifySemanticCalibration(ref Ref, raw []byte, model, dataset string) error {
	bounds := map[string][2]float64{
		"scene_cosine_min": {-1, 1}, "scene_log_luma_max": {0, 4}, "cluster_diameter_max": {0, 2}, "sharpness_scale": {1e-6, 1e6}, "clipping_penalty": {0, 10}, "match_inlier_min": {0, 1}, "match_error_px_max": {0, 100}, "identity_similarity_min": {-1, 1}, "landmark_drift_max": {0, 1}, "focus_warning": {0, 1}, "focus_critical": {0, 1}, "eye_open_min": {0, 1}, "glare_warning_fraction": {0, 1}, "clipping_warning_fraction": {0, 1}, "clipping_critical_fraction": {0, 1}, "depth_sigma": {.01, 1}, "registration_error_px": {.01, 10}, "motion_threshold": {.001, 1},
	}
	keys := []string{"schema_version", "model_lock_sha", "dataset_sha", "qualified", "normalization_scales", "loss_weights", "min_face_edge_px"}
	for k := range bounds {
		keys = append(keys, k)
	}
	fields, ok := semanticObject(raw, nil, keys...)
	hash := sha256.Sum256(raw)
	if len(raw) > 1<<20 || !ok || !validRef(ref) || hex.EncodeToString(hash[:]) != ref.SHA256 || !digestPattern.MatchString(model) || !digestPattern.MatchString(dataset) {
		return ErrInvalid
	}
	var version, edge int
	var qualified bool
	var pinnedModel, pinnedDataset string
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 || json.Unmarshal(fields["qualified"], &qualified) != nil || !qualified || json.Unmarshal(fields["model_lock_sha"], &pinnedModel) != nil || pinnedModel != model || json.Unmarshal(fields["dataset_sha"], &pinnedDataset) != nil || pinnedDataset != dataset || json.Unmarshal(fields["min_face_edge_px"], &edge) != nil || edge < 32 || edge > 512 {
		return ErrInvalid
	}
	for key, bound := range bounds {
		var value float64
		if json.Unmarshal(fields[key], &value) != nil || !finiteRange(value, bound[0], bound[1]) {
			return ErrInvalid
		}
	}
	var scales, weights []float64
	if json.Unmarshal(fields["normalization_scales"], &scales) != nil || len(scales) != 6 || json.Unmarshal(fields["loss_weights"], &weights) != nil || len(weights) != 6 {
		return ErrInvalid
	}
	var sum, correction float64
	for i := range scales {
		if !finiteRange(scales[i], 1e-6, 1e6) || !finiteRange(weights[i], 0, 1) {
			return ErrInvalid
		}
		y := weights[i] - correction
		t := sum + y
		correction = (t - sum) - y
		sum = t
	}
	if !finiteRange(sum, 1-1e-9, 1+1e-9) {
		return ErrInvalid
	}
	return nil
}
