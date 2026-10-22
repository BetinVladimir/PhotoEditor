package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

var ErrAuthoredSkinPending = errors.New("AUTHORED_SKIN_PENDING")

// Resume is the scheduler's concrete accepted-selection caller. Only the hash
// locator comes from outside: selected IDs and current source/producer proof are
// read under actual SQL authority. Pending is retriable preparation, never a
// failed phase, accepted review or fabricated completion.
func (p AuthoredSkinPreparation) Resume(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, image json.RawMessage, candidateSHA string, expected v2qualification.Expected, report v2input.Ref, limits AuthoredFaceBudget) (CurrentAuthoredSkin, error) {
	var zero CurrentAuthoredSkin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || who.UserID != source.Actor || who.SessionID != source.Session {
		return zero, v2input.ErrInvalid
	}
	selection, err := ReadCurrentAuthoredFaceSelection(ctx, p.Scheduler, who, source, candidateSHA)
	if err != nil {
		return zero, err
	}
	var state *string
	err = database.TenantTransaction(ctx, p.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.authored_render_skin_preparation_state($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, candidateSHA).Scan(&state)
	})
	if err != nil {
		return zero, err
	}
	if state == nil {
		return zero, v2input.ErrExpired
	}
	switch *state {
	case "absent", "draft":
		if err = p.Admit(ctx, who, source, image, selection, expected, report, limits); err != nil {
			return zero, err
		}
		return zero, ErrAuthoredSkinPending
	case "pending":
		return zero, ErrAuthoredSkinPending
	case "adopted":
		return p.Read(ctx, who, source, image, selection)
	default:
		return zero, v2input.ErrInvalid
	}
}
