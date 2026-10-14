package provenance

import (
	"bytes"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
)

// SigningRenderQualification records the historical measured identity retained
// by the adopted run. It is not permission to execute that runtime again.
type SigningRenderQualification struct {
	ModelLockSHA     string `json:"model_lock_sha"`
	RuntimeLockSHA   string `json:"runtime_lock_sha"`
	Backend          string `json:"backend"`
	DeviceProfileSHA string `json:"device_profile_sha"`
	DatasetSHA       string `json:"dataset_sha"`
	CalibrationSHA   string `json:"calibration_sha"`
}

// DecodeSigningRenderQualification accepts DB jsonb formatting but rejects
// duplicate/unknown/missing fields. Expected locks come from physical frozen
// Params, never from a request to the signing endpoint.
func DecodeSigningRenderQualification(raw []byte, expectedModel, expectedICC string, profileRaw []byte) (SigningRenderQualification, error) {
	var zero SigningRenderQualification
	if !digest(expectedModel) || !digest(expectedICC) || len(raw) < 2 || len(raw) > 4096 || len(profileRaw) < 2 || len(profileRaw) > 1<<20 {
		return zero, ErrInvalid
	}
	owned, err := canonical.Bytes(raw)
	if err != nil {
		return zero, ErrInvalid
	}
	var pin SigningRenderQualification
	decoder := json.NewDecoder(bytes.NewReader(owned))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pin) != nil || pin.ModelLockSHA != expectedModel || !pin.valid() {
		return zero, ErrInvalid
	}
	profileBytes, err := canonical.Bytes(profileRaw)
	if err != nil {
		return zero, ErrInvalid
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(profileBytes, &profile) != nil || profile == nil {
		return zero, ErrInvalid
	}
	expected := map[string]string{"model_lock_sha": pin.ModelLockSHA, "icc_lock_sha": expectedICC, "backend": pin.Backend, "render_runtime_sha": pin.RuntimeLockSHA, "renderer_version": "v2.2"}
	for key, value := range expected {
		var actual string
		if json.Unmarshal(profile[key], &actual) != nil || actual != value {
			return zero, ErrInvalid
		}
	}
	return pin, nil
}

func (p SigningRenderQualification) valid() bool {
	return digest(p.ModelLockSHA) && digest(p.RuntimeLockSHA) && digest(p.DeviceProfileSHA) && digest(p.DatasetSHA) && digest(p.CalibrationSHA) && (p.Backend == "cpu" || p.Backend == "cuda" || p.Backend == "mps")
}
