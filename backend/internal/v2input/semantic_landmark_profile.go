package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileSemanticLandmarkProfile accounts for exact declared downloads plus a
// conservative full-frame conversion/temporary envelope. Qualified costs must
// include installed FAN2D model and inference peaks; clients may not supply them.
// This is a capacity bound, not a GPU benchmark or scientific qualification.
func CompileSemanticLandmarkProfile(width, height, inputBytes int64, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	if width < 8 || height < 8 || width > 200000000 || height > 200000000 || width*height > 200000000 || inputBytes <= 0 || qualified.VRAMBytes <= 0 {
		return nil, ErrInvalid
	}
	for _, n := range []int64{inputBytes, qualified.RAMBytes, qualified.VRAMBytes, qualified.ScratchBytes, ramLimit, vramLimit, scratchLimit, outputLimit} {
		if n < 0 || n > safe {
			return nil, ErrInvalid
		}
	}
	// 128 bytes/pixel covers source conversion temporaries, converted RGB, alpha
	// exclusion, clipping and preview arrays. 1GiB also covers the worker minimum
	// and sequential face crops/landmark metadata; model peaks are additional.
	pixels := width * height
	output := int64(8 << 20) // bounded landmark document including per-face geometry
	ram := int64(1<<30) + 128*pixels + output
	scratch := output
	for _, term := range []int64{inputBytes, qualified.RAMBytes} {
		if term > safe-ram {
			return nil, ErrInvalid
		}
		ram += term
	}
	for _, term := range []int64{inputBytes, qualified.ScratchBytes} {
		if term > safe-scratch {
			return nil, ErrInvalid
		}
		scratch += term
	}
	if ram > ramLimit || qualified.VRAMBytes > vramLimit || scratch > scratchLimit || output > outputLimit {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{Backend: "cuda", RendererVersion: "v2.2", ResourceEstimate: sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: qualified.VRAMBytes, ScratchBytes: scratch, OutputBytes: output, PixelCount: pixels}})
}

// CompileSemanticLandmarkProfileFromInputs counts every declared artifact,
// including Params, exactly once. Catalog authorization remains caller duty.
func CompileSemanticLandmarkProfileFromInputs(width, height int64, params Ref, inputs []Ref, sizes []SemanticInputSize, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	total, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return nil, err
	}
	return CompileSemanticLandmarkProfile(width, height, total, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
}
