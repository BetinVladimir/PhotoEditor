package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// CompileSemanticDefectProfile estimates the existing CPU proposal worker.
// inputBytes must cover every distinct ready declared file, including Params;
// this pure compiler does not establish source closure or qualification.
// Full input/output residency plus 3GiB covers bounded sequential <=4MP ROIs.
func CompileSemanticDefectProfile(width, height, inputBytes int64, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	if width < 8 || height < 8 || width > 200000000 || height > 200000000 || width*height > 200000000 || inputBytes <= 0 {
		return nil, ErrInvalid
	}
	for _, n := range []int64{inputBytes, qualified.RAMBytes, qualified.VRAMBytes, qualified.ScratchBytes, ramLimit, vramLimit, scratchLimit, outputLimit} {
		if n < 0 || n > safe {
			return nil, ErrInvalid
		}
	}
	pixels := width * height
	// Matches prepare_proposals reserve and mask_png.png16_bound. Two bounded
	// control documents receive conservative additional output headroom.
	rawPNG := height * (2*width + 1)
	png := rawPNG + rawPNG/50 + (1 << 20)
	output := 6*pixels + 16384 + png + (24 << 20)
	ram := int64(3<<30) + output
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
	return canonical.Marshal(struct {
		Backend   string                  `json:"backend"`
		Renderer  string                  `json:"renderer_version"`
		Precision string                  `json:"precision"`
		MaxPixels int64                   `json:"max_pixels"`
		Estimate  sessionDescribeEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", "float32", 200000000, sessionDescribeEstimate{RAMBytes: ram, VRAMBytes: qualified.VRAMBytes, ScratchBytes: scratch, OutputBytes: output, PixelCount: pixels}})
}

// SemanticInputSize must come from an independently authorized ready catalog
// lookup. Exact accounting cannot establish authorization or input completeness.
type SemanticInputSize struct {
	Ref   Ref
	Bytes int64
}

// CompileSemanticDefectProfileFromInputs accounts for each declared file once,
// including the original Params. Missing, extra or ambiguous metadata fails.
func CompileSemanticDefectProfileFromInputs(width, height int64, params Ref, inputs []Ref, sizes []SemanticInputSize, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	total, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return nil, err
	}
	return CompileSemanticDefectProfile(width, height, total, qualified, ramLimit, vramLimit, scratchLimit, outputLimit)
}
