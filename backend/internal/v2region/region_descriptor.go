package v2region

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// RegionDescriptor is the public verification projection. It deliberately omits
// object locations, model inventories and the complete frozen recipe. A reader
// must prove live project access and admission before returning this projection.
type RegionDescriptor struct {
	SchemaVersion int                    `json:"schema_version"`
	AssetID       uuid.UUID              `json:"asset_id"`
	Request       RegionPreviewRequest   `json:"request"`
	Source        v2input.Ref            `json:"source"`
	Canvas        RegionDescriptorCanvas `json:"canvas"`
	ParamsSHA256  string                 `json:"params_sha256"`
	ICCSHA256     string                 `json:"icc_sha256"`
	Alpha         bool                   `json:"alpha"`
}
type RegionDescriptorCanvas struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}

// Descriptor uses verified full-source preparation evidence. targetICC must be
// supplied by server output policy, never request data or the output manifest.
// This method projects evidence; it does not grant access or prove admission.
func (p FrozenRegionPlan) Descriptor(targetICC string) (RegionDescriptor, error) {
	if !validRegionSHA(targetICC) {
		return RegionDescriptor{}, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return RegionDescriptor{}, err
	}
	shape, err := p.SourceShape()
	if err != nil {
		return RegionDescriptor{}, err
	}
	if document.AssetID == uuid.Nil || document.Request.VersionID == uuid.Nil || document.Source.Ref != document.FrozenRecipe.Source.Source {
		return RegionDescriptor{}, ErrRegionInvalid
	}
	return RegionDescriptor{SchemaVersion: 1, AssetID: document.AssetID, Request: document.Request, Source: document.Source.Ref, Canvas: RegionDescriptorCanvas{shape.Width, shape.Height}, ParamsSHA256: p.digest, ICCSHA256: targetICC, Alpha: shape.HasAlpha}, nil
}
