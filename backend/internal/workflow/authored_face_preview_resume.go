package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// A sealed preview is never re-reserved or restaged. Adopted means the actual
// dedicated physical adoption receipt readback is present, not a staged run ID.
func (s AuthoredFacePreviewRuntime) Resume(ctx context.Context, sha string, candidate v2render.AuthoredFaceSelectionCandidate, image []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil {
		return "", ErrAuthoredFaceUnavailable
	}
	raw, err := canonical.Marshal(candidate)
	if err != nil {
		return "", err
	}
	var state *string
	err = database.TenantTransaction(ctx, s.DB, candidate.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.authored_render_face_preview_preparation_state($1,$2,$3,$4,$5,$6,$7,$8)", candidate.Tenant, candidate.Parent, candidate.Ordinal, sha, string(raw), string(image), s.SRGBSHA, s.Policy).Scan(&state)
	})
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", v2input.ErrExpired
	}
	switch *state {
	case "absent", "draft":
		if _, err = s.Prepare(ctx, sha, candidate, image); err != nil {
			return "", err
		}
		return "pending", nil
	case "pending", "adopted":
		return *state, ctx.Err()
	default:
		return "", v2input.ErrInvalid
	}
}
