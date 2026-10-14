package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ReadSigningParentActor returns the original parent run creator only under
// the current scheduler lease and frozen required-provenance policy.
func (s ExpansionLeaseStore) ReadSigningParentActor(ctx context.Context,
	lease ExpansionLease) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil { return uuid.Nil, err }
	if s.DB == nil || !lease.valid() || lease.Cursor != lease.SlotCount {
		return uuid.Nil, v2input.ErrInvalid
	}
	var actor *uuid.UUID
	if err := s.DB.QueryRow(ctx,
		`SELECT photo.v31_delivery_signing_parent_actor($1,$2)`,
		lease.Expansion, lease.Token).Scan(&actor); err != nil {
		return uuid.Nil, err
	}
	if actor == nil || *actor == uuid.Nil { return uuid.Nil, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil { return uuid.Nil, err }
	return *actor, nil
}
