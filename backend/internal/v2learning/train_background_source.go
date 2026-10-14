package v2learning

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingBackgroundFreezer interface {
	Freeze(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.Ref, string, string, *string) ([]byte, error)
}
type TrainingBackgroundSource struct {
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
	Assets  TrainingBackgroundFreezer
}

// LoadBackground freezes current project/style asset authority once during
// example assembly. Evaluators may not resolve the portable ID again.
func (s TrainingBackgroundSource) LoadBackground(ctx context.Context, who auth.Identity, start TrainingStart, style v2input.Ref, icc string) (*v2draft.FrozenBackgroundAsset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Objects == nil || start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.UserID != start.ActorID || who.SessionID != start.AuthSessionID || !validManifestRef(style) || !validSHA(icc) {
		return nil, ErrInvalidTrainingDraft
	}
	root, err := s.Catalog.Lookup(ctx, who, start.TenantID, v2draft.LayerRef{Path: "TrainingRenderCase.base_style", Contract: "StyleV2", Ref: style})
	if err != nil {
		return nil, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, root, style)
	if err != nil {
		return nil, err
	}
	var fields struct {
		Extensions struct {
			Background *struct {
				Mode    string  `json:"mode"`
				AssetID *string `json:"asset_id"`
			} `json:"background"`
		} `json:"extensions"`
	}
	if json.Unmarshal(body, &fields) != nil {
		return nil, ErrInvalidTrainingDraft
	}
	background := fields.Extensions.Background
	if background == nil || background.Mode != "asset" {
		if err := validateFrozenBackgroundBinding(body, nil); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if background.AssetID == nil || *background.AssetID == "" || s.Assets == nil {
		return nil, ErrInvalidTrainingDraft
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// No implicit input-ICC assumption: missing ICC must fail the decoder unless
	// a future explicit authored policy is added to the immutable evidence.
	raw, err := s.Assets.Freeze(ctx, who, start.TenantID, start.ProjectID, style, *background.AssetID, icc, nil)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pin, err := v2draft.DecodeFrozenBackgroundAsset(raw)
	if err != nil || pin.PortableAssetID != *background.AssetID || !validTrainingBackgroundPin(&pin, icc, style) || validateFrozenBackgroundBinding(body, &pin) != nil {
		return nil, ErrInvalidTrainingDraft
	}
	return &pin, nil
}
