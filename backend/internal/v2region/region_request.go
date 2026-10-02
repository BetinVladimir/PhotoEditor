package v2region

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type RegionRecipeRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}
type RegionPreviewRequest struct {
	VersionID uuid.UUID       `json:"version_id"`
	Rect      RegionRect      `json:"rect"`
	Recipe    RegionRecipeRef `json:"recipe"`
	MaxEdge   int             `json:"max_edge"`
}

// DecodeRegionPreviewRequest validates the public contract only. It does not
// prove tenant/project access, recipe lineage, or ready object-store bytes.
func DecodeRegionPreviewRequest(raw []byte) (RegionPreviewRequest, error) {
	var out RegionPreviewRequest
	if len(raw) == 0 || len(raw) > 16384 {
		return out, ErrRegionInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return out, ErrRegionInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 4 {
		return out, ErrRegionInvalid
	}
	for _, name := range []string{"version_id", "rect", "recipe", "max_edge"} {
		if len(fields[name]) == 0 || string(fields[name]) == "null" {
			return out, ErrRegionInvalid
		}
	}
	for name, keys := range map[string][]string{"rect": {"x0", "y0", "x1", "y1"}, "recipe": {"artifact_id", "sha256"}} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(fields[name], &nested) != nil || len(nested) != len(keys) {
			return out, ErrRegionInvalid
		}
		for _, key := range keys {
			if len(nested[key]) == 0 || string(nested[key]) == "null" {
				return out, ErrRegionInvalid
			}
		}
	}
	if json.Unmarshal(body, &out) != nil || out.VersionID == uuid.Nil || out.Recipe.ArtifactID == uuid.Nil || len(out.Recipe.SHA256) != 64 {
		return RegionPreviewRequest{}, ErrRegionInvalid
	}
	for _, c := range out.Recipe.SHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return RegionPreviewRequest{}, ErrRegionInvalid
		}
	}
	if _, err = CompileRegionGeometry(1, 1, out.Rect, out.MaxEdge); err != nil {
		return RegionPreviewRequest{}, err
	}
	return out, nil
}
