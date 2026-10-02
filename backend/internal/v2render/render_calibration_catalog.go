package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"syscall"
)

// PinnedRenderCalibrationCatalog owns a bounded, immutable operator allowlist.
// Its SHA must be supplied independently by deployment configuration. A matching
// digest proves installed bytes, not that measurements were performed: operators
// must only admit independently qualified measured profiles into that allowlist.
type PinnedRenderCalibrationCatalog struct {
	profiles map[RenderQualification]RenderCalibration
}

func NewPinnedRenderCalibrationCatalog(raw []byte, expectedSHA string) (*PinnedRenderCalibrationCatalog, error) {
	if len(raw) < 2 || len(raw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	var manifest struct {
		SchemaVersion int                 `json:"schema_version"`
		Profiles      []RenderCalibration `json:"profiles"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != 1 || len(manifest.Profiles) < 1 || len(manifest.Profiles) > 128 {
		return nil, v2input.ErrInvalid
	}
	if err := validateRenderCalibrationManifestShape(raw); err != nil {
		return nil, err
	}
	result := &PinnedRenderCalibrationCatalog{profiles: make(map[RenderQualification]RenderCalibration, len(manifest.Profiles))}
	for _, profile := range manifest.Profiles {
		if profile.validate(profile.Qualification) != nil {
			return nil, v2input.ErrInvalid
		}
		if _, duplicate := result.profiles[profile.Qualification]; duplicate {
			return nil, v2input.ErrInvalid
		}
		profile.Samples = append([]RenderCalibrationSample(nil), profile.Samples...)
		result.profiles[profile.Qualification] = profile
	}
	return result, nil
}

func (c *PinnedRenderCalibrationCatalog) Resolve(ctx context.Context, q RenderQualification) (RenderCalibration, error) {
	if err := ctx.Err(); err != nil {
		return RenderCalibration{}, err
	}
	if c == nil {
		return RenderCalibration{}, renderUnavailable()
	}
	profile, ok := c.profiles[q]
	if !ok {
		return RenderCalibration{}, renderUnavailable()
	}
	profile.Samples = append([]RenderCalibrationSample(nil), profile.Samples...)
	return profile, nil
}

var _ RenderCalibrationCatalog = (*PinnedRenderCalibrationCatalog)(nil)

// LoadPinnedRenderCalibrationCatalog bounds file IO before parsing. Nonblocking
// open plus regular-file checks reject pipes/devices; one descriptor pins reads
// across pathname replacement. Atomic deployment replacement applies on restart.
func LoadPinnedRenderCalibrationCatalog(path, expectedSHA string) (*PinnedRenderCalibrationCatalog, error) {
	if !filepath.IsAbs(path) {
		return nil, v2input.ErrInvalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 1048576 {
		return nil, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1048577))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, v2input.ErrInvalid
	}
	// Detect concurrent mutation of the same inode; digest remains authoritative.
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, v2input.ErrInvalid
	}
	return NewPinnedRenderCalibrationCatalog(raw, expectedSHA)
}

// encoding/json otherwise accepts case aliases and decodes null/missing integer
// fields as zero. Measured zero must be explicit; omission is never evidence.
func renderCalibrationObject(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(keys) {
		return nil, v2input.ErrInvalid
	}
	for _, key := range keys {
		value, ok := object[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, v2input.ErrInvalid
		}
	}
	return object, nil
}

func validateRenderCalibrationManifestShape(raw []byte) error {
	root, err := renderCalibrationObject(raw, "schema_version", "profiles")
	if err != nil {
		return err
	}
	var profiles []json.RawMessage
	if json.Unmarshal(root["profiles"], &profiles) != nil {
		return v2input.ErrInvalid
	}
	for _, rawProfile := range profiles {
		profile, err := renderCalibrationObject(rawProfile, "qualification", "evidence_sha", "icc_lock_sha", "tile_size", "alpha_mode", "samples")
		if err != nil {
			return err
		}
		if _, err := renderCalibrationObject(profile["qualification"], "model_lock_sha", "runtime_lock_sha", "backend", "device_profile_sha", "dataset_sha"); err != nil {
			return err
		}
		var samples []json.RawMessage
		if json.Unmarshal(profile["samples"], &samples) != nil {
			return v2input.ErrInvalid
		}
		for _, rawSample := range samples {
			sample, err := renderCalibrationObject(rawSample, "width", "height", "provider_peak")
			if err != nil {
				return err
			}
			if _, err := renderCalibrationObject(sample["provider_peak"], "ram_bytes", "scratch_bytes", "vram_bytes", "output_bytes"); err != nil {
				return err
			}
		}
	}
	return nil
}
