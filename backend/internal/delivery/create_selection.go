package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ResolveSelection validates a previously pinned ordered recipe list. This is
// an internal coordinator input, not an extension of the public CreateRequest.
func (i CreateIntent) ResolveSelection(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, recipes []v2input.Ref) ([]v2input.SessionSnapshotItem, error) {
	invalid := func() ([]v2input.SessionSnapshotItem, error) { return nil, domain.NewError("VALIDATION_FAILED", 422) }
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil || len(recipes) < 1 || len(recipes) > 5000 {
		return invalid()
	}
	owned := append([]v2input.Ref(nil), recipes...)
	for _, ref := range owned {
		if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) {
			return invalid()
		}
	}
	body, err := canonical.Marshal(owned)
	if err != nil {
		return invalid()
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.resolve_v30_delivery_selection($1,$2,$3,$4,$5,$6::jsonb)`, tid, i.request.ProjectID, i.request.SnapshotID, who.UserID, who.SessionID, string(body)).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, domain.NewError("DELIVERY_RECIPE_SELECTION_STALE", 409)
	}
	var wire struct {
		Snapshot uuid.UUID                     `json:"snapshot_id"`
		Items    []v2input.SessionSnapshotItem `json:"items"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.Snapshot != i.request.SnapshotID || len(wire.Items) != len(owned) {
		return nil, domain.NewError("DELIVERY_SELECTION_INVALID", 500)
	}
	if _, _, err := v2input.CompilePinnedSessionInputSnapshot(wire.Snapshot, wire.Items); err != nil {
		return nil, domain.NewError("DELIVERY_SELECTION_INVALID", 500)
	}
	for index, item := range wire.Items {
		if item.Recipe != owned[index] {
			return nil, domain.NewError("DELIVERY_SELECTION_INVALID", 500)
		}
	}
	return wire.Items, nil
}
