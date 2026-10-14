package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileSemanticFaceParseProfile accounts for exact declared downloads plus a
// conservative full-frame conversion/temporary envelope. Qualified costs must
// include installed BiSeNet model and inference peaks; clients may not supply them.
// This is a capacity bound, not a GPU benchmark or scientific qualification.
func CompileSemanticFaceParseProfile(width, height, inputBytes int64, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
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
	// exclusion, clipping, labels, confidence and restored per-face arrays. 1GiB covers the worker minimum
	// and sequential 19-class crop logits; model peaks are additional.
	pixels := width * height
	output := 2*pixels + 8192 + (1 << 20) // uint16 NPY and bounded FaceLabels19 manifest
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

// CompileSemanticFaceParseProfileFromInputs counts every declared artifact,
// including Params, exactly once. Catalog authorization remains caller duty.
func CompileSemanticFaceParseProfileFromInputs(width, height int64, params Ref, inputs []Ref, sizes []SemanticInputSize, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	total, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return nil, err
	}
	return CompileSemanticFaceParseProfile(width, height, total, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
}
