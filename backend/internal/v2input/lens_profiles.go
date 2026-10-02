package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileLensProfile matches optical.resources.lens_resources. Original EXIF
// materialization finishes before lens buffers open; reserve the larger peak.
func CompileLensProfile(width, height int64, strength float64, originalBytes, ramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	if width < 1 || height < 1 || width > 200000000 || height > 200000000 || width*height > 200000000 ||
		!finiteRange(strength, 0, 1) || originalBytes < 1 || originalBytes > 9007199254740991-(64<<20) {
		return nil, ErrInvalid
	}
	pixels := width * height
	outputChannels := int64(28)
	if strength == 0 {
		outputChannels = 4
	}
	estimate := sessionDescribeEstimate{RAMBytes: 60*pixels + (512 << 20), VRAMBytes: 0,
		ScratchBytes: 60*pixels + (64 << 20), OutputBytes: outputChannels*pixels + (32 << 20), PixelCount: pixels}
	if peak := originalBytes + (64 << 20); peak > estimate.ScratchBytes {
		estimate.ScratchBytes = peak
	}
	if ramLimit < estimate.RAMBytes || scratchLimit < estimate.ScratchBytes || outputLimit < estimate.OutputBytes {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{"cpu", "v2.2", estimate})
}
