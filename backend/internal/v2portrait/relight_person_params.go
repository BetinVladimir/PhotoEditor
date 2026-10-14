package v2portrait

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// VerifyRelightPersonProducerParams must use the exact original reference
// returned by live SQL origin discovery, not a caller-selected replacement.
func VerifyRelightPersonProducerParams(params RelightParams, ref v2input.Ref, raw []byte) error {
	if _, _, err := params.Compile(); err != nil {
		return err
	}
	if len(raw) < 2 || len(raw) > 65536 || !validLocalBase(1, params.Base, ref, ref, 0) {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	var original struct {
		Image           v2input.Ref `json:"image"`
		ModelID         string      `json:"model_id"`
		ModelLockID     string      `json:"model_lock_id"`
		PreprocessingID string      `json:"preprocessing_id"`
		LongSide        int         `json:"long_side"`
		GuidedTile      int         `json:"guided_tile"`
	}
	if json.Unmarshal(raw, &original) != nil || original.Image != params.Base.Source || original.ModelID != "lraspp_mobilenet_v3_large" || original.PreprocessingID != "person_guided_v1" || original.LongSide != 768 || (original.GuidedTile != 256 && original.GuidedTile != 512 && original.GuidedTile != 1024) {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(original.ModelLockID); err != nil {
		return v2input.ErrInvalid
	}
	normalized, err := canonical.Marshal(original)
	if err != nil || !bytes.Equal(normalized, raw) {
		return v2input.ErrInvalid
	}
	return nil
}
