package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type RenderBatchTicket struct {
	RunID       uuid.UUID   `json:"run_id"`
	State       string      `json:"state"`
	ChildRunIDs []uuid.UUID `json:"child_run_ids"`
}

func decodeRenderBatchTicket(batch uuid.UUID, raw []byte) (RenderBatchTicket, error) {
	var out RenderBatchTicket
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || ValidateRenderBatchTicket(batch, out) != nil {
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	return out, nil
}

// ActivateSubmission atomically seals every child, synchronizes parent state and
// saves the exact 202 ticket. A completed replay bypasses draft/terminal sealing.
func (s SQLRenderDraftStore) ActivateSubmission(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity, key, requestSHA string) (RenderBatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return RenderBatchTicket{}, err
	}
	expected, err := v2input.RunID(tenant, who.UserID, key)
	if s.DB == nil || err != nil || expected != batch || who.SessionID == uuid.Nil {
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(requestSHA); err != nil {
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	var out RenderBatchTicket
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		out, err = activateRenderSubmissionInTransaction(ctx, tx, tenant, batch, who, key, requestSHA)
		return err
	})
	if err != nil {
		return RenderBatchTicket{}, err
	}
	return out, nil
}

func activateRenderSubmissionInTransaction(ctx context.Context, tx pgx.Tx, tenant, batch uuid.UUID, who auth.Identity, key, requestSHA string) (RenderBatchTicket, error) {
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return RenderBatchTicket{}, err
	}
	if role != "photo_scheduler" {
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	var project uuid.UUID
	err := tx.QueryRow(ctx, "SELECT (manifest->>'project_id')::uuid FROM photo.v26_render_batch_manifest($1,$2,$3,$4)", tenant, batch, who.UserID, who.SessionID).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		return RenderBatchTicket{}, v2input.ErrExpired
	}
	if err != nil {
		return RenderBatchTicket{}, err
	}
	var outcome string
	if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.probe_v26_render_submission($1,$2,$3,$4,$5,$6,$7)", tenant, project, who.UserID, who.SessionID, batch, key, requestSHA).Scan(&outcome); err != nil {
		return RenderBatchTicket{}, err
	}
	switch outcome {
	case "pending", "replayed":
	case "conflict":
		return RenderBatchTicket{}, v2input.ErrConflict
	case "expired", "missing":
		return RenderBatchTicket{}, v2input.ErrExpired
	default:
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.read_v26_render_submission_ticket($1,$2,$3,$4,$5,$6)", tenant, batch, who.UserID, who.SessionID, key, requestSHA).Scan(&raw); err != nil {
		return RenderBatchTicket{}, err
	}
	if len(raw) > 0 {
		return decodeRenderBatchTicket(batch, raw)
	}
	if err := activateRenderBatchInTransaction(ctx, tx, tenant, batch, who); err != nil {
		return RenderBatchTicket{}, err
	}
	if err := tx.QueryRow(ctx, "SELECT photo.complete_v26_render_submission($1,$2,$3,$4,$5,$6)", tenant, batch, who.UserID, who.SessionID, key, requestSHA).Scan(&raw); err != nil {
		return RenderBatchTicket{}, err
	}
	if len(raw) == 0 {
		return RenderBatchTicket{}, renderUnavailable()
	}
	return decodeRenderBatchTicket(batch, raw)
}

// ValidateRenderBatchTicket rejects mismatched or unordered admission responses.
func ValidateRenderBatchTicket(batch uuid.UUID, out RenderBatchTicket) error {
	if batch == uuid.Nil || out.RunID != batch || len(out.ChildRunIDs) < 1 || len(out.ChildRunIDs) > 5000 || (out.State != "queued" && out.State != "running" && out.State != "waiting_review") {
		return v2input.ErrInvalid
	}
	for i, id := range out.ChildRunIDs {
		if id != uuid.NewSHA1(batch, []byte("render.batch:"+strconv.Itoa(i))) {
			return v2input.ErrInvalid
		}
	}
	return nil
}
