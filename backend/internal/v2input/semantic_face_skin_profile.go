package v2input

import (
	"encoding/json"

	"photoeditor/backend/internal/canonical"
)

// CompileSemanticFaceSkinProfile bounds the existing CPU worker's uint16 map,
// float32 probability and mapped output, temporary comparisons and PNG encoder.
// Input bytes are exact catalog file sizes; this is not scientific qualification.
func CompileSemanticFaceSkinProfile(width, height, inputBytes, ramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	if width < 1 || height < 1 || width > 200000000 || height > 200000000 || width*height > 200000000 || inputBytes < 1 {
		return nil, ErrInvalid
	}
	for _, value := range []int64{inputBytes, ramLimit, scratchLimit, outputLimit} {
		if value < 0 || value > safe {
			return nil, ErrInvalid
		}
	}
	pixels := width * height
	// Same formula as algorithms.mask_png.png16_bound, including filter bytes.
	pngRaw := height * (width*2 + 1)
	pngBound := pngRaw + pngRaw/50 + (1 << 20)
	// Float32 NPY, bounded header, PNG and bounded MaskRef control document.
	output := 4*pixels + 8192 + pngBound + (1 << 20)
	ramBase := int64(256<<20) + 16*pixels
	if inputBytes > safe-ramBase || inputBytes > safe-output {
		return nil, ErrInvalid
	}
	ram, scratch := ramBase+inputBytes, output+inputBytes
	if ram > ramLimit || scratch > scratchLimit || output > outputLimit {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{Backend: "cpu", RendererVersion: "v2.2", ResourceEstimate: sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: 0, ScratchBytes: scratch, OutputBytes: output, PixelCount: pixels}})
}

// Counts own Params and every declared file exactly once, without authorizing
// catalog access or checking physical label values.
func CompileSemanticFaceSkinProfileFromInputs(width, height int64, params Ref, inputs []Ref, sizes []SemanticInputSize, ramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	total, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return nil, err
	}
	return CompileSemanticFaceSkinProfile(width, height, total, ramLimit, scratchLimit, outputLimit)
}
