package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// ActivateBatch seals every declared child in one DB-only transaction. A later
// refusal rolls back earlier seals/tasks/events/outbox; publication is separate.
// Parent aggregate status remains governed by the existing phase synchronizer.
func (s SQLRenderDraftStore) ActivateBatch(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		return activateRenderBatchInTransaction(ctx, tx, tenant, batch, who)
	})
}

func activateRenderBatchInTransaction(ctx context.Context, tx pgx.Tx, tenant, batch uuid.UUID, who auth.Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var raw []byte
	var manifestSHA string
	var cancelled bool
	err := tx.QueryRow(ctx, "SELECT manifest,manifest_sha,cancel_requested FROM photo.v26_render_batch_manifest($1,$2,$3,$4)", tenant, batch, who.UserID, who.SessionID).Scan(&raw, &manifestSHA, &cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.ErrExpired
	}
	if err != nil {
		return err
	}
	if cancelled {
		return v2input.ErrExpired
	}
	manifest, err := DecodeRenderBatchManifest(batch, who.SessionID, raw, manifestSHA)
	if err != nil {
		return err
	}
	for _, part := range manifest.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		var sealed bool
		if err := tx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", tenant, part.RunID).Scan(&sealed); err != nil {
			return err
		}
		if !sealed {
			return renderUnavailable()
		}
	}
	return nil
}
