package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileAuthoredMaskTransformProfile conservatively bounds the existing CPU
// float32 transform/refine/PNG pipeline. These are resource admission bounds,
// never a fabricated scientific qualification or a backend relabel.
func CompileAuthoredMaskTransformProfile(sw, sh, tw, th, inputBytes, ramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	for _, n := range []int64{sw, sh, tw, th} {
		if n < 1 || n > 200000000 {
			return nil, ErrInvalid
		}
	}
	if sw*sh > 200000000 || tw*th > 200000000 {
		return nil, ErrInvalid
	}
	for _, n := range []int64{inputBytes, ramLimit, scratchLimit, outputLimit} {
		if n < 0 || n > safe {
			return nil, ErrInvalid
		}
	}
	if inputBytes == 0 {
		return nil, ErrInvalid
	}
	pixels := tw * th
	maximum := pixels
	if sw*sh > maximum {
		maximum = sw * sh
	}
	// Existing png16_bound includes filter bytes, compression overhead and 1MiB.
	raw := th * (tw*2 + 1)
	png := raw + raw/50 + (1 << 20)
	// Published float32 array + bounded NPY header, PNG and canonical JSON root.
	output := 4*pixels + 8192 + png + (1 << 20)
	// transform_prepare retains raw+refined float32 arrays concurrently and PNG.
	scratchBase := 8*pixels + 8192 + png + (16 << 20)
	// >=worker's required 1GiB. 128 bytes per max source/target pixel cover mapped
	// arrays/guide and tile filter intermediates; full input bytes counted once.
	ramBase := int64(1<<30) + 128*maximum
	if inputBytes > safe-ramBase || inputBytes > safe-scratchBase {
		return nil, ErrInvalid
	}
	ram, scratch := ramBase+inputBytes, scratchBase+inputBytes
	if ram > ramLimit || scratch > scratchLimit || output > outputLimit {
		return nil, ErrInvalid
	}
	return canonical.Marshal(struct {
		Backend  string                  `json:"backend"`
		Renderer string                  `json:"renderer_version"`
		Tile     int                     `json:"tile_size"`
		Index    int64                   `json:"input_index_scratch_bytes"`
		Estimate sessionDescribeEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", 512, 16 << 20, sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: 0, ScratchBytes: scratch, OutputBytes: output, PixelCount: maximum}})
}
