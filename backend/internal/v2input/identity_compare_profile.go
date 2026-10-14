package v2input

import (
	"encoding/json"

	"photoeditor/backend/internal/canonical"
)

// CompileIdentityCompareProfile reserves the two full-resolution images,
// guards, changed-mask output, bounded face previews and exact input bytes.
// Qualified costs are installed operator peaks, never client-provided values.
func CompileIdentityCompareProfile(backend string, width, height, inputBytes int64, qualified SemanticQualifiedCosts,
	ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	if (backend != "cpu" && backend != "cuda") || width < 8 || height < 8 || width > 200000000 || height > 200000000 ||
		width*height > 200000000 || inputBytes <= 0 ||
		(backend == "cpu" && qualified.VRAMBytes != 0) ||
		(backend == "cuda" && qualified.VRAMBytes <= 0) {
		return nil, ErrInvalid
	}
	for _, value := range []int64{inputBytes, qualified.RAMBytes, qualified.VRAMBytes, qualified.ScratchBytes,
		ramLimit, vramLimit, scratchLimit, outputLimit} {
		if value < 0 || value > safe {
			return nil, ErrInvalid
		}
	}
	pixels := width * height
	maskOutput := 4*pixels + 4096 // float32 NPY; matches output policy ceiling.
	if maskOutput > 800004096 {
		return nil, ErrInvalid
	}
	output := maskOutput + (16 << 20) // MaskRef and bounded IdentityReport.
	ram := int64(2<<30) + 160*pixels + output
	scratch := inputBytes + output + (64 << 20)
	for _, term := range []int64{inputBytes, qualified.RAMBytes} {
		if term > safe-ram {
			return nil, ErrInvalid
		}
		ram += term
	}
	if qualified.ScratchBytes > safe-scratch {
		return nil, ErrInvalid
	}
	scratch += qualified.ScratchBytes
	if ram > ramLimit || qualified.VRAMBytes > vramLimit || scratch > scratchLimit || output > outputLimit {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{Backend: backend, RendererVersion: "v2.2",
		ResourceEstimate: sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: qualified.VRAMBytes,
			ScratchBytes: scratch, OutputBytes: output, PixelCount: pixels}})
}

// Every closure artifact, including Params, must have a trusted bounded size.
func CompileIdentityCompareProfileFromInputs(backend string, width, height int64, params Ref, inputs []Ref,
	sizes []SemanticInputSize, qualified SemanticQualifiedCosts,
	ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	total, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return nil, err
	}
	return CompileIdentityCompareProfile(backend, width, height, total, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
}
