package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type GlareDonorFaceChoice struct {
	SourceFaceID uuid.UUID `json:"source_face_id"`
	DonorFaceID  uuid.UUID `json:"donor_face_id"`
}

// ReadGlareDonorFaceChoice resolves current confirmed observations inside a
// scheduler transaction. SQL also runs the existing donor proof builder, so
// IDs are returned only with live source, consent and shoot-session authority.
func ReadGlareDonorFaceChoice(ctx context.Context, db DonorConfirmationTransactions,
	who auth.Identity, tenant, project, shoot, run uuid.UUID,
	paramsRaw []byte, sourceObservation, donorObservation uuid.UUID) (GlareDonorFaceChoice, error) {
	var zero GlareDonorFaceChoice
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil || run == uuid.Nil ||
		sourceObservation == uuid.Nil || donorObservation == uuid.Nil ||
		sourceObservation == donorObservation || len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(normal, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	if err = tx.QueryRow(ctx,
		"SELECT photo.v28_glare_donor_face_choice($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)",
		tenant, project, shoot, run, who.UserID, who.SessionID,
		string(paramsRaw), sourceObservation, donorObservation).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 1024 {
		return zero, v2input.ErrExpired
	}
	var choice GlareDonorFaceChoice
	if json.Unmarshal(raw, &choice) != nil || choice.SourceFaceID == uuid.Nil ||
		choice.DonorFaceID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(choice)
	canonicalRaw, normalizeErr := canonical.Bytes(raw)
	if err != nil || normalizeErr != nil || !bytes.Equal(exact, canonicalRaw) {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return choice, nil
}
