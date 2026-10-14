package provenance

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ActivateSigningInTransaction follows fresh SigningActivationPreparation and
// committed physical transport publication. Caller owns scheduler transaction
// and must roll back every failure and expose the ticket only after commit.
// Native/operator checks must never execute inside this database transaction.
func ActivateSigningInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, transport SigningInputTransport, bucket string) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, ErrInvalid
	}
	if err := transport.Register(ctx, tx, who, ready, prepared, bucket); err != nil {
		return zero, err
	}
	task, err := canonical.Marshal(prepared.compiled.Task())
	if err != nil {
		return zero, err
	}
	manifest, err := canonical.Marshal(transport.Built().Manifest)
	if err != nil {
		return zero, err
	}
	i := prepared.intent
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.activate_v31_signing($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, string(task), string(manifest)).Scan(&accepted); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !accepted {
		return zero, artifactjson.ErrStale
	}
	return v2input.JobTicket{RunID: i.run, WorkflowID: i.workflow, State: "queued"}, nil
}
