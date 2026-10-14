package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// CaptureDraftSelection is an observation inside the caller's authorized
// transaction. Persist the returned exact list before FreezeRecipeSelection;
// rerunning capture on retry would substitute mutable current draft revisions.
func (i CreateIntent) CaptureDraftSelection(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID) ([]CreateRecipePin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.capture_v30_delivery_draft_selection($1,$2,$3,$4,$5)`, tid, i.request.ProjectID, i.request.SnapshotID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, domain.NewError("DELIVERY_DRAFT_SELECTION_STALE", 409)
	}
	if len(raw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	var pins []CreateRecipePin
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pins) != nil || decoder.Decode(new(any)) != io.EOF || len(pins) < 1 || len(pins) > 5000 {
		return nil, v2input.ErrInvalid
	}
	seen := make(map[uuid.UUID]bool)
	for index, pin := range pins {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pin.Ordinal != index || pin.Binding.AssetVersionID == uuid.Nil || seen[pin.Binding.AssetVersionID] || pin.Binding.Source.ArtifactID == uuid.Nil || !shaPattern.MatchString(pin.Binding.Source.SHA256) || !shaPattern.MatchString(pin.Binding.CanvasHash) || pin.Request.AssumeProfile != nil {
			return nil, v2input.ErrInvalid
		}
		seen[pin.Binding.AssetVersionID] = true
		if _, err := v2render.RenderFreezeDocumentID(tid, who.UserID, pin.Asset, "capture-validation", pin.Request); err != nil {
			return nil, err
		}
	}
	return pins, nil
}
