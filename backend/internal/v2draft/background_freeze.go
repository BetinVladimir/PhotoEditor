package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
)

type FrozenBackgroundAsset struct {
	SchemaVersion   int         `json:"schema_version"`
	PortableAssetID string      `json:"portable_asset_id"`
	StyleVersionID  uuid.UUID   `json:"style_version_id"`
	Style           v2input.Ref `json:"style"`
	Source          v2input.Ref `json:"source"`
	ExpectedBytes   int64       `json:"expected_bytes"`
	MediaType       string      `json:"media_type"`
	Suffix          string      `json:"suffix"`
	AssumeProfile   *string     `json:"assume_profile"`
	WorkICCSHA      string      `json:"work_icc_sha"`
}

// FreezeBackgroundAsset compiles independently authorized resolver evidence.
// It does not substitute for live admission or normalized pixel verification.
func FreezeBackgroundAsset(asset PortableBackgroundAsset, style v2input.Ref, workICC string, assume *string) ([]byte, error) {
	var ownedAssume *string
	if assume != nil {
		value := *assume
		ownedAssume = &value
	}
	pin := FrozenBackgroundAsset{1, asset.PortableID, asset.StyleVersionID, style, asset.Ref, asset.Object.Bytes, asset.Object.ContentType, map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/tiff": ".tiff"}[asset.Object.ContentType], ownedAssume, workICC}
	if asset.Object.SHA256 != asset.Ref.SHA256 || asset.Object.VersionID == "" || asset.Object.VersionID == "null" || asset.Object.Key == "" {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(pin)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeFrozenBackgroundAsset(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func DecodeFrozenBackgroundAsset(raw []byte) (FrozenBackgroundAsset, error) {
	var out FrozenBackgroundAsset
	invalid := func() (FrozenBackgroundAsset, error) { return FrozenBackgroundAsset{}, v2input.ErrInvalid }
	if len(raw) > 16384 || json.Unmarshal(raw, &out) != nil {
		return invalid()
	}
	actual, err := canonical.Marshal(out)
	if err != nil || !bytes.Equal(actual, raw) || out.SchemaVersion != 1 || out.StyleVersionID == uuid.Nil || !isDocumentRef(out.Style) || !isDocumentRef(out.Source) || !correctionDigest(out.WorkICCSHA) || out.ExpectedBytes < 1 || out.ExpectedBytes > 536870912 || out.PortableAssetID == "" || len(out.PortableAssetID) > 120 || strings.TrimSpace(out.PortableAssetID) != out.PortableAssetID {
		return invalid()
	}
	expected, ok := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/tiff": ".tiff"}[out.MediaType]
	if !ok || expected != out.Suffix {
		return invalid()
	}
	if out.AssumeProfile != nil && *out.AssumeProfile != "srgb_v1" && *out.AssumeProfile != "linear_srgb_v1" && *out.AssumeProfile != "work_rec2020_gamma24_v1" {
		return invalid()
	}
	return out, nil
}

// Freeze resolves current project/style authority before compiling a pin.
// The returned bytes still need publishing and admission-time reauthorization.
func (s SQLBackgroundAssetResolver) Freeze(ctx context.Context, who auth.Identity,
	tenantID, projectID uuid.UUID, style v2input.Ref, portableID, workICC string, assume *string) ([]byte, error) {
	asset, err := s.Resolve(ctx, who, tenantID, projectID, style, portableID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return FreezeBackgroundAsset(asset, style, workICC, assume)
}
