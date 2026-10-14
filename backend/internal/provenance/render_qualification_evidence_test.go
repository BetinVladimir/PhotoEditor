package provenance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSigningRenderQualificationBindsHistoricalProfile(t *testing.T) {
	model, icc := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, mode := range []string{"valid", "missing", "unknown", "duplicate", "null", "model", "runtime", "icc", "backend", "renderer"} {
		t.Run(mode, func(t *testing.T) {
			pin := SigningRenderQualification{ModelLockSHA: model, RuntimeLockSHA: strings.Repeat("c", 64), Backend: "mps", DeviceProfileSHA: strings.Repeat("d", 64), DatasetSHA: strings.Repeat("e", 64), CalibrationSHA: strings.Repeat("f", 64)}
			profile := map[string]any{"model_lock_sha": model, "icc_lock_sha": icc, "backend": "mps", "render_runtime_sha": pin.RuntimeLockSHA, "renderer_version": "v2.2", "tile_size": 512}
			raw, _ := json.Marshal(pin)
			switch mode {
			case "missing":
				var fields map[string]any
				json.Unmarshal(raw, &fields)
				delete(fields, "dataset_sha")
				raw, _ = json.Marshal(fields)
			case "unknown":
				raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
			case "duplicate":
				raw = append(raw[:len(raw)-1], []byte(`,"backend":"mps"}`)...)
			case "null":
				raw = []byte(`null`)
			case "model":
				profile["model_lock_sha"] = strings.Repeat("b", 64)
			case "runtime":
				profile["render_runtime_sha"] = strings.Repeat("d", 64)
			case "icc":
				profile["icc_lock_sha"] = strings.Repeat("c", 64)
			case "backend":
				profile["backend"] = "cuda"
			case "renderer":
				profile["renderer_version"] = "v2.3"
			}
			profileRaw, _ := json.Marshal(profile)
			result, err := DecodeSigningRenderQualification(raw, model, icc, profileRaw)
			if mode == "valid" {
				if err != nil || result != pin {
					t.Fatal("valid retained qualification rejected", err)
				}
				return
			}
			if err == nil || result != (SigningRenderQualification{}) {
				t.Fatal("foreign or malformed qualification accepted")
			}
		})
	}
}
