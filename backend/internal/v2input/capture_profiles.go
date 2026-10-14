package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileCapturePrepareProfile admits one mapped source and one 2048px proxy.
// It preserves the full-resolution source; scratch accounts for materializing
// its declared NPY arrays, while RAM accounts for the bounded proxy and models.
func CompileCapturePrepareProfile(backend string, width, height int64, hasAlpha bool,
	ramLimit, vramLimit, scratchLimit int64) (json.RawMessage, error) {
	if (backend != "cpu" && backend != "cuda" && backend != "mps") || width < 1 || height < 1 || width > 200000000 || height > 200000000 || width*height > 200000000 {
		return nil, ErrInvalid
	}
	w, h := width, height
	if w > 2048 || h > 2048 {
		// Match Python round() for integer ratios, including tie-to-even cases.
		longest := w
		if h > longest {
			longest = h
		}
		roundRatio := func(n int64) int64 {
			q, r := n/longest, n%longest
			if 2*r > longest || 2*r == longest && q%2 == 1 {
				q++
			}
			if q < 1 {
				q = 1
			}
			return q
		}
		w, h = roundRatio(width*2048), roundRatio(height*2048)
	}
	channels := int64(12)
	if hasAlpha {
		// Float32 optical alpha can flow into capture proxies; reserve four
		// bytes per pixel even for historical uint16 inputs.
		channels = 16
	}
	vram := int64(0)
	if backend == "cuda" || backend == "mps" {
		vram = 6 << 30
	}
	proxyPixels := w * h
	estimate := sessionDescribeEstimate{
		RAMBytes:     2<<30 + 64*proxyPixels,
		VRAMBytes:    vram,
		ScratchBytes: channels*(width*height+proxyPixels) + (16 << 20),
		OutputBytes:  channels*proxyPixels + (16 << 20),
		PixelCount:   width * height,
	}
	if ramLimit < estimate.RAMBytes || vramLimit < vram || scratchLimit < estimate.ScratchBytes {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{backend, "v2.2", estimate})
}
