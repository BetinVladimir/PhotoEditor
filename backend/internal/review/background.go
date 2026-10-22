package review

import "github.com/google/uuid"

// ApplyBackgroundAsset is the accepted-style portable binding frozen at admission.
// It contains immutable artifact identity, never a client URL or object locator.
type ApplyBackgroundAsset struct {
 PortableID string `json:"portable_asset_id"`
 StyleVersionID uuid.UUID `json:"style_version_id"`
 Style ArtifactRef `json:"style"`
 Source ArtifactRef `json:"source"`
 ExpectedBytes int64 `json:"expected_bytes"`
 Suffix string `json:"suffix"`
}
