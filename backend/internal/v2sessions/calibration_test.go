package v2sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCalibrationContractAndSessionPolicy(t *testing.T) {
	lock := strings.Repeat("a", 64)
	value := map[string]any{
		"schema_version": 1, "model_lock_sha": lock, "dataset_sha": strings.Repeat("b", 64), "qualified": true,
		"scene_cosine_min": .3, "scene_log_luma_max": 1., "cluster_diameter_max": 1.,
		"sharpness_scale": 1., "clipping_penalty": 1., "match_inlier_min": .3,
		"match_error_px_max": 5., "identity_similarity_min": .4, "landmark_drift_max": .2,
		"normalization_scales": []float64{1, 1, 1, 1, 1, 1}, "loss_weights": []float64{.4, .1, .1, .1, .2, .1},
		"focus_warning": .5, "focus_critical": .2, "eye_open_min": .5,
		"glare_warning_fraction": .1, "clipping_warning_fraction": .1,
		"clipping_critical_fraction": .3, "min_face_edge_px": 64,
		"depth_sigma": .1, "registration_error_px": 2., "motion_threshold": .1,
	}
	check := func() error {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return validateCalibration(raw, lock, strings.Repeat("b", 64))
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}

	validWeights := value["loss_weights"]
	for _, weights := range [][]float64{{0, 0, 0, 0, 0, 0}, {.5, .5, .5, .5, .5, .5}, {.1, .1, .1, .1, .1, .1}} {
		value["loss_weights"] = weights
		if check() == nil {
			t.Fatal("accepted non-normalized loss weights", weights)
		}
	}
	value["loss_weights"] = validWeights
	canonicalBody, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonicalBody)
	digest := hex.EncodeToString(sum[:])
	if edge, err := CalibrationMinFaceEdge(canonicalBody, digest, lock, strings.Repeat("b", 64)); err != nil || edge != 64 {
		t.Fatalf("pinned face threshold: %d, %v", edge, err)
	}
	for _, pins := range [][3]string{{strings.Repeat("f", 64), lock, strings.Repeat("b", 64)}, {digest, "invalid", strings.Repeat("b", 64)}, {digest, lock, strings.Repeat("c", 64)}} {
		if edge, err := CalibrationMinFaceEdge(canonicalBody, pins[0], pins[1], pins[2]); err == nil || edge != 0 {
			t.Fatal("exposed threshold with invalid pins")
		}
	}
	unqualified := make(map[string]any)
	for k, v := range value {
		unqualified[k] = v
	}
	unqualified["qualified"] = false
	badBody, err := canonical.Marshal(unqualified)
	if err != nil {
		t.Fatal(err)
	}
	badSum := sha256.Sum256(badBody)
	if edge, err := CalibrationMinFaceEdge(badBody, hex.EncodeToString(badSum[:]), lock, strings.Repeat("b", 64)); err == nil || edge != 0 {
		t.Fatal("exposed unqualified threshold")
	}
	if err = verifyCalibrationBytes(canonicalBody, digest, lock, strings.Repeat("b", 64)); err != nil {
		t.Fatal("valid pinned calibration", err)
	}
	if verifyCalibrationBytes(canonicalBody, strings.Repeat("f", 64), lock, strings.Repeat("b", 64)) == nil {
		t.Fatal("accepted calibration with different SHA")
	}
	if verifyCalibrationBytes(append([]byte(" "), canonicalBody...), digest, lock, strings.Repeat("b", 64)) == nil {
		t.Fatal("accepted non-canonical calibration bytes")
	}
	value["model_lock_sha"] = strings.Repeat("c", 64)
	if check() == nil {
		t.Fatal("accepted different model lock")
	}
	value["model_lock_sha"] = lock
	value["dataset_sha"] = strings.Repeat("c", 64)
	if check() == nil {
		t.Fatal("accepted calibration from different qualified dataset")
	}
	value["dataset_sha"] = strings.Repeat("b", 64)
	value["normalization_scales"] = []float64{1, 1, 1, 1, 1}
	if check() == nil {
		t.Fatal("accepted incomplete normalization scales")
	}
	policy := SessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60, NeighborCount: 4,
		ProtectManualPicks: true, CameraOffsets: []CameraOffset{},
		Calibration: ArtifactRef{ArtifactID: uuid.New(), SHA256: lock}}
	if !policyValid(policy) {
		t.Fatal("rejected valid session policy")
	}
	policy.CameraOffsets = nil
	if policyValid(policy) {
		t.Fatal("accepted missing camera_offsets")
	}
	if _, err := parseCapture(nil); err == nil {
		t.Fatal("accepted absent capture_policy")
	}
	if p, err := parseCapture(json.RawMessage("null")); err != nil || p != nil {
		t.Fatalf("nullable capture: %v", err)
	}
}
