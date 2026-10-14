package v2draft

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
	"strings"
)

// SelectRenderBackgroundAsset follows worker whole-module replacement semantics.
// styleBody must be the accepted StyleV2; sourceSHA is the encoded source SHA
// from the verified WorkingImage, not the WorkingImage document digest. This
// proves byte identity and ordered selection, not SQL approval or model closure.
// An empty result means that the effective background needs no imported asset.
func SelectRenderBackgroundAsset(authored []byte, inputs ResolvedRecipeInputs,
	styleBody []byte, overrideBodies [][]byte, sourceSHA string) (string, error) {
	baseline, extensions, err := resolveRenderModuleDocuments(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return "", err
	}
	effective := inputs
	effective.Style = baseline
	if _, err := ResolveRecipe(authored, effective); err != nil {
		return "", err
	}
	return backgroundPortableID(extensions)
}

func backgroundPortableID(raw []byte) (string, error) {
	var extensions map[string]json.RawMessage
	if json.Unmarshal(raw, &extensions) != nil || extensions == nil {
		return "", v2input.ErrInvalid
	}
	value, ok := extensions["background"]
	if !ok || string(value) == "null" {
		return "", nil
	}
	var background struct {
		Mode           string    `json:"mode"`
		AssetID        *string   `json:"asset_id"`
		SolidRGB       []float64 `json:"solid_rgb"`
		BlurStrength   *float64  `json:"blur_strength"`
		ProtectSubject *bool     `json:"protect_subject"`
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&background) != nil || background.BlurStrength == nil || !inRange(*background.BlurStrength, 0, 1) || background.ProtectSubject == nil || !*background.ProtectSubject {
		return "", v2input.ErrInvalid
	}
	switch background.Mode {
	case "asset":
		if background.AssetID == nil || *background.AssetID == "" || len(*background.AssetID) > 120 || strings.TrimSpace(*background.AssetID) != *background.AssetID {
			return "", v2input.ErrInvalid
		}
		return *background.AssetID, nil
	case "solid":
		if len(background.SolidRGB) != 3 {
			return "", v2input.ErrInvalid
		}
		for _, v := range background.SolidRGB {
			if !inRange(v, 0, 1) {
				return "", v2input.ErrInvalid
			}
		}
	case "keep", "transparent", "blur", "generate":
	default:
		return "", v2input.ErrInvalid
	}
	return "", nil
}
