package review

import (
	"encoding/json"
	"errors"
)

var ErrInvalidDepth = errors.New("invalid relative depth")

type RelativeDepth struct {
	Artifact       ArtifactRef     `json:"artifact"`
	Float32Shape   []int64         `json:"float32_shape"`
	InvalidMask    *ArtifactRef    `json:"invalid_mask"`
	Canvas         Canvas          `json:"canvas"`
	TransformChain json.RawMessage `json:"transform_chain"`
	Normalization  string          `json:"normalization"`
}

func ParseRelativeDepth(raw []byte) (RelativeDepth, error) {
	var depth RelativeDepth
	if err := decodeExact(raw, &depth); err != nil {
		return RelativeDepth{}, err
	}
	if !depth.Artifact.valid() || !validCanvas(depth.Canvas) || depth.Normalization != "minmax_valid" ||
		len(depth.Float32Shape) != 2 || depth.Float32Shape[0] != depth.Canvas.Height || depth.Float32Shape[1] != depth.Canvas.Width ||
		depth.InvalidMask != nil && !depth.InvalidMask.valid() {
		return RelativeDepth{}, ErrInvalidDepth
	}
	if err := validateTransformChain(depth.TransformChain, depth.Canvas); err != nil {
		return RelativeDepth{}, err
	}
	return depth, nil
}
