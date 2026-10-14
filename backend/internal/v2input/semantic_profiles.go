package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// SemanticQualifiedCosts are additional installed operator/model and transport
// peaks, excluding the native stage, five array downloads and RGB conversion
// costs calculated below. They are trusted qualification inputs, not client data.
type SemanticQualifiedCosts struct {
	RAMBytes     int64
	VRAMBytes    int64
	ScratchBytes int64
}

func CompileSemanticTransferProfile(referenceWidth, referenceHeight, targetWidth, targetHeight int64,
	referenceGamma, targetGamma bool, qualified SemanticQualifiedCosts, ramLimit, vramLimit, scratchLimit, outputLimit int64) (json.RawMessage, error) {
	const safe int64 = 9007199254740991
	for _, shape := range [][2]int64{{referenceWidth, referenceHeight}, {targetWidth, targetHeight}} {
		if shape[0] < 8 || shape[1] < 8 || shape[0] > 200000000 || shape[1] > 200000000 || shape[0]*shape[1] > 200000000 {
			return nil, ErrInvalid
		}
	}
	for _, value := range []int64{qualified.RAMBytes, qualified.VRAMBytes, qualified.ScratchBytes, ramLimit, vramLimit, scratchLimit, outputLimit} {
		if value < 0 || value > safe {
			return nil, ErrInvalid
		}
	}
	source, target := referenceWidth*referenceHeight, targetWidth*targetHeight
	conversions := int64(0)
	if referenceGamma {
		conversions += 12*source + 8192
	}
	if targetGamma {
		conversions += 12*target + 8192
	}
	downloads := 12*(source+target) + 8*source + 4*target + 5*8192
	estimate := sessionDescribeEstimate{RAMBytes: 20*(source+target) + (320 << 20) + conversions,
		ScratchBytes: 4*target + 8192 + (8 << 20) + downloads + conversions, OutputBytes: 4*target + 4096 + (24 << 20), PixelCount: source + target}
	if qualified.RAMBytes > safe-estimate.RAMBytes || qualified.ScratchBytes > safe-estimate.ScratchBytes {
		return nil, ErrInvalid
	}
	estimate.RAMBytes += qualified.RAMBytes
	estimate.ScratchBytes += qualified.ScratchBytes
	estimate.VRAMBytes = qualified.VRAMBytes
	if estimate.OutputBytes > 825169920 || ramLimit < estimate.RAMBytes || vramLimit < estimate.VRAMBytes || scratchLimit < estimate.ScratchBytes || outputLimit < estimate.OutputBytes {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{"cuda", "v2.2", estimate})
}
