package delivery

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// CreateRecipePin is server-observed selection evidence, not public request
// input. The coordinator must durably pin this ordered list before object IO.
type CreateRecipePin struct {
	Ordinal int                           `json:"ordinal"`
	Asset   uuid.UUID                     `json:"asset_id"`
	Binding v2input.SessionVersionBinding `json:"binding"`
	Request v2render.RenderFreezeRequest  `json:"request"`
}

type CreateRecipeFreezer interface {
	Freeze(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2render.RenderFreezeRequest) (idempotency.Response, error)
}

// FreezeRecipeSelection freezes exact selected draft revisions using the
// existing V26 producer. It never chooses a latest recipe. A later SQL
// ResolveSelection must bind these refs to the delivery snapshot inventory.
// Partial child publication is replayable under stable per-item keys; no
// partial selection escapes a failure and no delivery run is dispatched here.
func (i CreateIntent) FreezeRecipeSelection(ctx context.Context, who auth.Identity, pins []CreateRecipePin, freezer CreateRecipeFreezer) ([]v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil || freezer == nil || len(pins) < 1 || len(pins) > 5000 {
		return nil, v2input.ErrInvalid
	}
	owned := append([]CreateRecipePin(nil), pins...)
	keys := make([]string, len(owned))
	ids := make([]uuid.UUID, len(owned))
	seen := make(map[uuid.UUID]bool)
	for index := range owned {
		pin := &owned[index]
		if pin.Ordinal != index || pin.Asset == uuid.Nil || pin.Binding.AssetVersionID == uuid.Nil || seen[pin.Binding.AssetVersionID] || pin.Binding.Source.ArtifactID == uuid.Nil || !shaPattern.MatchString(pin.Binding.Source.SHA256) || !shaPattern.MatchString(pin.Binding.CanvasHash) {
			return nil, v2input.ErrInvalid
		}
		seen[pin.Binding.AssetVersionID] = true
		if pin.Request.AssumeProfile != nil {
			value := *pin.Request.AssumeProfile
			pin.Request.AssumeProfile = &value
		}
		keys[index] = uuid.NewSHA1(i.identity.Run, []byte("delivery:recipe:"+strconv.Itoa(index)+":"+pin.Asset.String())).String()
		id, err := v2render.RenderFreezeDocumentID(i.tenant, who.UserID, pin.Asset, keys[index], pin.Request)
		if err != nil {
			return nil, err
		}
		ids[index] = id
	}
	refs := make([]v2input.Ref, len(owned))
	for index, pin := range owned {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := freezer.Freeze(ctx, who, i.tenant, pin.Asset, keys[index], pin.Request)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.Status != 200 || result.ResourceID == nil || *result.ResourceID != ids[index] {
			return nil, v2input.ErrInvalid
		}
		frozen, err := v2render.DecodeRenderFreezeResult(result.Body, ids[index], pin.Request)
		if err != nil || frozen.Source != pin.Binding {
			return nil, v2input.ErrInvalid
		}
		refs[index] = frozen.Recipe
	}
	return refs, nil
}
