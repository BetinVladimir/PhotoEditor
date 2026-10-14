package v2portrait

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RelightAnalysisParams compiles the existing worker ABI. It grants no source,
// model qualification, capacity or task admission authority.
func RelightAnalysisParams(operation string, source v2input.Ref, modelLockSHA string) ([]byte, string, error) {
	if !validRelightTransportRef(source) {
		return nil, "", v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(modelLockSHA); err != nil {
		return nil, "", v2input.ErrInvalid
	}
	params := map[string]any{"image": source, "model_lock_id": modelLockSHA}
	switch operation {
	case "depth.predict":
		params["model_id"] = "depth_anything_v2_small"
		params["preprocessing_id"] = "depth_anything_v2_small_518_v1"
		params["input_size"] = 518
	case "mask.person":
		params["model_id"] = "lraspp_mobilenet_v3_large"
		params["preprocessing_id"] = "person_guided_v1"
		params["long_side"] = 768
		params["guided_tile"] = 512
	default:
		return nil, "", v2input.ErrInvalid
	}
	body, err := canonical.Marshal(params)
	if err != nil {
		return nil, "", err
	}
	sha, err := canonical.Hash(params)
	return body, sha, err
}
