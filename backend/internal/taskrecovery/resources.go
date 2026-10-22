package taskrecovery

import (
	"bytes"
	"encoding/json"
	"errors"
)

type ResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

func ParseResourceEstimate(profile []byte) (ResourceEstimate, error) {
	var p map[string]json.RawMessage
	var e ResourceEstimate
	if err := json.Unmarshal(profile, &p); err != nil {
		return e, errors.New("invalid execution profile")
	}
	raw, ok := p["resource_estimate"]
	if !ok {
		return e, errors.New("missing resource estimate")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 5 || keys["ram_bytes"] == nil || keys["vram_bytes"] == nil || keys["scratch_bytes"] == nil || keys["output_bytes"] == nil || keys["pixel_count"] == nil {
		return e, errors.New("incomplete resource estimate")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	for _, value := range keys {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return e, errors.New("null resource estimate")
		}
	}
	if err := d.Decode(&e); err != nil {
		return e, errors.New("invalid resource estimate")
	}
	const max = int64(1<<53 - 1)
	if e.RAMBytes <= 0 || e.RAMBytes > max || e.VRAMBytes < 0 || e.VRAMBytes > max || e.ScratchBytes < 0 || e.ScratchBytes > max || e.OutputBytes < 0 || e.OutputBytes > max || e.PixelCount < 0 || e.PixelCount > max {
		return e, errors.New("invalid resource estimate range")
	}
	pixelLimit := int64(200000000)
	if raw, ok := p["max_pixels"]; ok {
		// An explicit zero cap admits only non-pixel work (PixelCount == 0).
		// It is a hard zero bound, never an unlimited/default sentinel.
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &pixelLimit) != nil || pixelLimit < 0 || pixelLimit > max {
			return e, errors.New("invalid pixel limit")
		}
	}
	if e.PixelCount > pixelLimit {
		return e, errors.New("output pixels limit")
	}
	return e, nil
}
