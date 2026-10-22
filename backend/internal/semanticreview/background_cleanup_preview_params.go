package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
)

// BackgroundCleanupPreviewParams carries a proposal; it grants no Apply approval.
type BackgroundCleanupPreviewParams struct {
	ExposurePreviewParams
	Kind          string `json:"kind"`
	ModelID       string `json:"model_id"`
	ContextPixels int    `json:"context_pixels"`
	FeatherPixels int    `json:"feather_pixels"`
}

func FreezeBackgroundCleanupPreviewParams(p BackgroundCleanupPreviewParams) ([]byte, string, error) {
	if _, _, err := FreezeExposurePreviewParams(p.ExposurePreviewParams); err != nil {
		return nil, "", err
	}
	if p.Kind != "background_cleanup" || p.ModelID != "big_lama" || p.ContextPixels < 32 || p.ContextPixels > 512 || p.FeatherPixels < 1 || p.FeatherPixels > 64 {
		return nil, "", ErrInvalid
	}
	raw, err := canonical.Marshal(p)
	if err != nil {
		return nil, "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// ParseBackgroundCleanupPreviewParams requires the exact canonical worker document.
func ParseBackgroundCleanupPreviewParams(raw []byte) (BackgroundCleanupPreviewParams, error) {
	var p BackgroundCleanupPreviewParams
	if len(raw) < 2 || len(raw) > 1048576 || decodeStrict(raw, &p) != nil {
		return BackgroundCleanupPreviewParams{}, ErrInvalid
	}
	body, _, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil || !bytes.Equal(raw, body) {
		return BackgroundCleanupPreviewParams{}, ErrInvalid
	}
	return p, nil
}
