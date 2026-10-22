package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// Capacity envelope includes full-frame conversion, model temporaries, output,
// every declared input and additional installed model peaks. It is not evidence
// of scientific quality or actual device qualification.
func CompileSemanticAnalysisProfile(operation string, width, height int64, alpha bool, inputBytes int64, pins SemanticAnalysisRuntimePins, costs SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	return CompileSemanticAnalysisProfileForBackend("cuda", operation, width, height, alpha, inputBytes, pins, costs, ramLimit, vramLimit, scratchLimit, outputLimit)
}

// Backend must come from the independently verified qualification Expected.
func CompileSemanticAnalysisProfileForBackend(backend, operation string, width, height int64, alpha bool, inputBytes int64, pins SemanticAnalysisRuntimePins, costs SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	if backend != "cuda" && backend != "mps" {
		return nil, ErrInvalid
	}
	if width < 8 || height < 8 || width > 200000000 || height > 200000000 || width*height > 200000000 || inputBytes < 1 || costs.VRAMBytes < 1 || (backend == "cuda" && costs.VRAMBytes > 10_000_000_000) {
		return nil, ErrInvalid
	}
	if !digestPattern.MatchString(pins.ModelLockSHA) || !digestPattern.MatchString(pins.RuntimeLockSHA) || !digestPattern.MatchString(pins.DeviceProfileSHA) {
		return nil, ErrInvalid
	}
	for _, value := range []int64{inputBytes, costs.RAMBytes, costs.VRAMBytes, costs.ScratchBytes, ramLimit, vramLimit, scratchLimit, outputLimit} {
		if value < 0 || value > safe {
			return nil, ErrInvalid
		}
	}
	pixels := width * height
	perPixel, outputPerPixel := int64(128), int64(2)
	switch operation {
	case "scene.parse":
	case "depth.predict":
		outputPerPixel = 4
		if alpha {
			perPixel += 24
			outputPerPixel += 4
		}
	default:
		return nil, ErrInvalid
	}
	out := outputPerPixel*pixels + 16384 + (1 << 20)
	ram := int64(1<<30) + perPixel*pixels + out
	// decode_logits permits 150x1024x1024 outputs. Its float32 logits,
	// subtraction/exp/probability temporaries and finite mask coexist even for
	// a tiny source image; reserve independently of source pixel count.
	if operation == "scene.parse" {
		ram += 3 << 30
	}
	// The v2 input index stays reserved throughout adapter execution. Its
	// minimum reservation is independent of source dimensions.
	const inputIndexScratch int64 = 16 << 20
	if out > safe-inputIndexScratch {
		return nil, ErrInvalid
	}
	scratch := out + inputIndexScratch
	for _, value := range []int64{inputBytes, costs.RAMBytes} {
		if value > safe-ram {
			return nil, ErrInvalid
		}
		ram += value
	}
	for _, value := range []int64{inputBytes, costs.ScratchBytes} {
		if value > safe-scratch {
			return nil, ErrInvalid
		}
		scratch += value
	}
	if ram > ramLimit || costs.VRAMBytes > vramLimit || scratch > scratchLimit || out > outputLimit {
		return nil, ErrInvalid
	}
	return canonical.Marshal(struct {
		Backend          string                  `json:"backend"`
		RendererVersion  string                  `json:"renderer_version"`
		ResourceEstimate sessionDescribeEstimate `json:"resource_estimate"`
		ModelLockSHA     string                  `json:"model_lock_sha"`
		RuntimeLockSHA   string                  `json:"runtime_lock_sha"`
		DeviceProfileSHA string                  `json:"device_profile_sha"`
	}{backend, "v2.2", sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: costs.VRAMBytes, ScratchBytes: scratch, OutputBytes: out, PixelCount: pixels}, pins.ModelLockSHA, pins.RuntimeLockSHA, pins.DeviceProfileSHA})
}
