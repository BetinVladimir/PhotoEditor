package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type perspectiveLinesSubmissionState struct {
	Stage string     `json:"stage"`
	Task  *uuid.UUID `json:"task_id"`
}

func parsePerspectiveLinesSubmissionState(raw []byte) (perspectiveLinesSubmissionState, error) {
	var zero perspectiveLinesSubmissionState
	if len(raw) < 2 || len(raw) > 1024 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var state perspectiveLinesSubmissionState
	if json.Unmarshal(normal, &state) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(state)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	switch state.Stage {
	case "missing", "conflict":
		if state.Task != nil {
			return zero, v2input.ErrInvalid
		}
	case "draft", "sealed", "queued", "running", "waiting_review", "completed", "expired", "draft_expired":
		if state.Task == nil || *state.Task == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
	default:
		return zero, v2input.ErrInvalid
	}
	return state, nil
}
func (s PerspectiveLinesPreparation) submissionState(ctx context.Context, who auth.Identity, tenant, project, run uuid.UUID, intent PerspectiveLinesSubmissionIntent) (perspectiveLinesSubmissionState, error) {
	var zero perspectiveLinesSubmissionState
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || len(s.Policy.body) < 2 {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(intent.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_submission_state($1,$2,$3,$4,$5,$6,$7::jsonb,$8)", tenant, project, run, who.UserID, who.SessionID, intent.Snapshot, string(base), string(s.Policy.body)).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	return parsePerspectiveLinesSubmissionState(raw)
}

func (s PerspectiveLinesPreparation) bindSubmission(ctx context.Context, who auth.Identity, tenant, project, run uuid.UUID, key string, intent PerspectiveLinesSubmissionIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return err
	}
	if _, err := DecodePerspectiveLinesSubmissionIntent(raw); err != nil {
		return err
	}
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	var status string
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.bind_v29_perspective_lines_submission($1,$2,$3,$4,$5,$6,$7)", tenant, project, run, who.UserID, who.SessionID, key, string(raw)).Scan(&status)
	})
	if err != nil {
		return err
	}
	return perspectiveLinesBindingStatus(status)
}
func perspectiveLinesBindingStatus(status string) error {
	switch status {
	case "bound":
		return nil
	case "conflict":
		return v2input.ErrConflict
	case "expired":
		return v2input.ErrExpired
	default:
		return v2input.ErrInvalid
	}
}
