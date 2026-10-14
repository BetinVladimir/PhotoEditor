package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func donorCalibrationFixture(t *testing.T) donorCalibrationEvidence {
	t.Helper()
	model, dataset := strings.Repeat("a", 64), strings.Repeat("b", 64)
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": model, "dataset_sha": dataset, "qualified": true,
		"scene_cosine_min": .3, "scene_log_luma_max": 1., "cluster_diameter_max": 1.,
		"sharpness_scale": 1., "clipping_penalty": 1., "match_inlier_min": .3, "match_error_px_max": 5.,
		"identity_similarity_min": .4, "landmark_drift_max": .2,
		"normalization_scales": []float64{1, 1, 1, 1, 1, 1}, "loss_weights": []float64{.4, .1, .1, .1, .2, .1},
		"focus_warning": .5, "focus_critical": .2, "eye_open_min": .5, "glare_warning_fraction": .1,
		"clipping_warning_fraction": .1, "clipping_critical_fraction": .3, "min_face_edge_px": 64,
		"depth_sigma": .1, "registration_error_px": 2., "motion_threshold": .1,
	})
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return donorCalibrationEvidence{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, raw: raw, modelLockSHA: model, datasetSHA: dataset}
}

func TestDonorCalibrationPin(t *testing.T) {
	pin := donorCalibrationFixture(t)
	if err := pin.verify(64); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"threshold", "hash", "model", "dataset", "bytes", "id"} {
		bad := pin
		bad.raw = append([]byte(nil), pin.raw...)
		edge := 64.
		switch mode {
		case "threshold":
			edge = 32
		case "hash":
			bad.ref.SHA256 = strings.Repeat("c", 64)
		case "model":
			bad.modelLockSHA = strings.Repeat("c", 64)
		case "dataset":
			bad.datasetSHA = strings.Repeat("c", 64)
		case "bytes":
			bad.raw[0] ^= 1
		case "id":
			bad.ref.ArtifactID = uuid.Nil
		}
		if err := bad.verify(edge); err == nil {
			t.Fatal("accepted changed calibration", mode)
		}
	}
}

func TestCalibratedDonorPreflight(t *testing.T) {
	pin := donorCalibrationFixture(t)
	catalog := &donorCompositionCatalog{}
	resolver := ClosureResolver{Catalog: catalog}
	for _, mode := range []string{"hash", "model", "id", "cancelled"} {
		ref, model, ctx := pin.ref, pin.modelLockSHA, context.Background()
		switch mode {
		case "hash":
			ref.SHA256 = strings.Repeat("c", 64)
		case "model":
			model = "invalid"
		case "id":
			ref.ArtifactID = uuid.Nil
		case "cancelled":
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		got, err := resolver.PrepareCalibratedDonorInputEvidence(ctx, auth.Identity{}, uuid.New(), LoadedDonorTaskPlan{}, catalog, DonorManualProducer{}, ref, pin.raw, model, pin.datasetSHA)
		if err == nil || got.Entries != nil || got.calibration != nil || catalog.calls != 0 {
			t.Fatal("invalid calibration escaped preflight", mode, err)
		}
	}
}
