package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
)

// RetouchPreviewParams carries a proposal; it grants no Apply approval.
type RetouchPreviewParams struct {
	ExposurePreviewParams
	Kind string `json:"kind"`
}

func FreezeRetouchPreviewParams(p RetouchPreviewParams) ([]byte, string, error) {
	if _, _, err := FreezeExposurePreviewParams(p.ExposurePreviewParams); err != nil {
		return nil, "", err
	}
	if p.Kind != "retouch_region" {
		return nil, "", ErrInvalid
	}
	raw, err := canonical.Marshal(p)
	if err != nil {
		return nil, "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// ParseRetouchPreviewParams requires the exact canonical worker document.
func ParseRetouchPreviewParams(raw []byte) (RetouchPreviewParams, error) {
	var p RetouchPreviewParams
	if len(raw) < 2 || len(raw) > 1048576 || decodeStrict(raw, &p) != nil {
		return RetouchPreviewParams{}, ErrInvalid
	}
	body, _, err := FreezeRetouchPreviewParams(p)
	if err != nil || !bytes.Equal(raw, body) {
		return RetouchPreviewParams{}, ErrInvalid
	}
	return p, nil
}
