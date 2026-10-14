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

func parsePerspectiveLinesStoredResponse(raw []byte, run uuid.UUID) (*PerspectiveLinesSubmissionReceipt, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if len(raw) > 2048 || run == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	var receipt PerspectiveLinesSubmissionReceipt
	if json.Unmarshal(normal, &receipt) != nil {
		return nil, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(receipt)
	if err != nil || !bytes.Equal(exact, normal) || receipt.Run != run || receipt.Task == uuid.Nil || receipt.State != "queued" {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil || receipt.Workflow != workflow {
		return nil, v2input.ErrInvalid
	}
	return &receipt, nil
}
func (s PerspectiveLinesPreparation) storedSubmissionResponse(ctx context.Context, who auth.Identity, tenant, project, run uuid.UUID, key string, intent PerspectiveLinesSubmissionIntent) (*PerspectiveLinesSubmissionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return nil, err
	}
	var response []byte
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_submission_response($1,$2,$3,$4,$5,$6,$7,$8)", tenant, project, run, who.UserID, who.SessionID, key, string(raw), string(s.Policy.body)).Scan(&response)
	})
	if err != nil {
		return nil, err
	}
	return parsePerspectiveLinesStoredResponse(response, run)
}
