package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// CheckDraft validates the compiled config's exact canonical representation and
// repeats live original snapshot/upload/task authority for all nodes. It does
// not persist shells or grant proposal/mask access. Admission must repeat SQL
// validation under its own locks; this read-only result is never an access token.
func (s SQLOriginalCompositionPreflight) CheckDraft(ctx context.Context, tenant, run uuid.UUID, who auth.Identity, draft CompiledCompositionDraft) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if (draft.Config.QueueClass!=""&&draft.Config.QueueClass!="batch") || s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(draft.Body) < 2 || len(draft.Body) > 1048576 || draft.OutputBytes < 1 || draft.OutputBytes > 9007199254740991 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(draft.Config)
	if err != nil || !bytes.Equal(body, draft.Body) {
		return v2input.ErrInvalid
	}
	sha, err := canonical.Hash(draft.Config)
	if err != nil || sha != draft.SHA256 {
		return v2input.ErrInvalid
	}
	for _, node := range draft.Config.Composition.Nodes {
		if len(node.ParamsJCS) < 2 || len(node.ParamsJCS) > 1048576 {
			return v2input.ErrInvalid
		}
		params, err := canonical.Bytes([]byte(node.ParamsJCS))
		if err != nil || string(params) != node.ParamsJCS {
			return v2input.ErrInvalid
		}
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return err
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_plan_scope($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`, tenant, draft.Config.ProjectID, draft.Config.SnapshotID, who.UserID, who.SessionID, workflow, json.RawMessage(body), draft.OutputBytes).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
