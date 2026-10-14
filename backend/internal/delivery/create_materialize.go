package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// MaterializePrepare requires a complete independently authorized closure,
// already compiled into this FrozenPrepareTask. It creates only a blocked task;
// transport publication, proof/package commit and sealing remain separate.
func (i CreateIntent) MaterializePrepare(ctx context.Context, tx ProofTransaction, who auth.Identity, tid uuid.UUID, frozen FrozenPrepareTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tid != i.tenant || who.UserID != i.actor || who.SessionID == uuid.Nil || i.identity.Run == uuid.Nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	if draft.ID != i.identity.PrepareTask || draft.ParamsArtifactID != i.identity.Params {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	body, err := canonical.Marshal(draft)
	if err != nil {
		return err
	}
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.materialize_v30_delivery_prepare($1,$2,$3,$4,$5,$6::jsonb)`, tid, who.UserID, who.SessionID, i.key, i.requestSHA, string(body)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_PREPARE_MATERIALIZATION_STALE", 409)
	}
	return nil
}
