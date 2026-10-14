package semanticproposal

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Terminal-only observation survives cancelled producer rows. It never grants
// authority to consume a producer result or to publish/admit a parent graph.
func (s SQLTransferSourceReader) readNativeInitialTerminalState(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || parent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	var operation *string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT operation FROM photo.v27_native_initial_terminal_state($1,$2,$3,$4,$5)", tenantID, s.ProjectID, who.UserID, who.SessionID, parent).Scan(&operation)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", v2input.ErrConflict
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if operation == nil {
		return "", nil
	}
	switch *operation {
	case "face.detect", "mask.person", "face.landmarks", "face.parse", "mask.face_skin", "defects.propose":
		return *operation, nil
	}
	return "", v2input.ErrConflict
}
