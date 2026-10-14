package delivery

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// NextSigningPage advances only the separate discovery cursor. The SQL
// function verifies the current parent lease, complete export admission and
// frozen required-provenance policy before returning a bounded page start.
func (s ExpansionLeaseStore) NextSigningPage(ctx context.Context,
	lease ExpansionLease, limit int) (int, error) {
	if err := ctx.Err(); err != nil { return 0, err }
	if s.DB == nil || !lease.valid() || lease.Cursor != lease.SlotCount ||
		limit < 1 || limit > 100 {
		return 0, v2input.ErrInvalid
	}
	var start *int
	if err := s.DB.QueryRow(ctx,
		`SELECT photo.next_v31_delivery_signing_page($1,$2,$3)`,
		lease.Expansion, lease.Token, limit).Scan(&start); err != nil {
		return 0, err
	}
	if start == nil || *start < 0 || *start >= lease.SlotCount {
		return 0, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil { return 0, err }
	return *start, nil
}
