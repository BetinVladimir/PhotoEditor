package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSemanticCalibrationMatchesPinnedFullContract(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("fixtures")
	}
	var values map[string]any
	if json.Unmarshal(fixtures.Fixtures["Calibration"], &values) != nil {
		t.Fatal("calibration")
	}
	values["qualified"] = true
	values["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	model, dataset := values["model_lock_sha"].(string), values["dataset_sha"].(string)
	encode := func(v map[string]any) (Ref, []byte) {
		body, err := canonical.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		sha, err := canonical.Hash(v)
		if err != nil {
			t.Fatal(err)
		}
		return Ref{uuid.New(), sha}, body
	}
	ref, body := encode(values)
	if err := VerifySemanticCalibration(ref, body, model, dataset); err != nil {
		t.Fatal(err)
	}
	if err := VerifySemanticCalibration(ref, append(body, ' '), model, dataset); err == nil {
		t.Fatal("raw hash mismatch accepted")
	}
	if err := VerifySemanticCalibration(ref, body, strings.Repeat("f", 64), dataset); err == nil {
		t.Fatal("foreign model accepted")
	}
	if err := VerifySemanticCalibration(ref, body, model, strings.Repeat("e", 64)); err == nil {
		t.Fatal("foreign dataset accepted")
	}
	for name, value := range map[string]any{"qualified": false, "loss_weights": []float64{1, 1, 1, 1, 1, 1}, "normalization_scales": []float64{1}, "match_error_px_max": 101, "depth_sigma": nil, "min_face_edge_px": 32.5, "unexpected": true} {
		changed := map[string]any{}
		for k, v := range values {
			changed[k] = v
		}
		changed[name] = value
		pin, raw := encode(changed)
		if err := VerifySemanticCalibration(pin, raw, model, dataset); err == nil {
			t.Fatal("invalid fresh-pinned field accepted", name)
		}
	}
}
