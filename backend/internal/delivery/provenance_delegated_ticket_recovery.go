package delivery

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RecoverDelegatedSignerTicket reads only a committed, sealed signer child
// bound to the current parent slot. It is an acknowledgement, not job success.
func RecoverDelegatedSignerTicket(ctx context.Context, db *pgxpool.Pool,
	lease ExpansionLease, slot Slot,
	scope SignerDelegationScope) (v2input.JobTicket, bool, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil { return zero, false, err }
	if db == nil || ValidateSignerDelegationScope(lease, slot, scope) != nil {
		return zero, false, v2input.ErrInvalid
	}
	var raw []byte
	err := db.QueryRow(ctx,`SELECT photo.recover_v31_delivery_signer_ticket(
	 $1,$2,$3,$4)`,lease.Expansion,lease.Token,slot.Ordinal,
		slot.PresetID).Scan(&raw)
	if err != nil { return zero, false, err }
	if len(raw) == 0 { return zero, false, ctx.Err() }
	if len(raw) > 4096 { return zero, false, v2input.ErrInvalid }
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil { return zero, false, v2input.ErrInvalid }
	var ticket v2input.JobTicket
	if err := json.Unmarshal(canonicalBody, &ticket); err != nil {
		return zero, false, v2input.ErrInvalid
	}
	if ticket.WorkflowID != scope.WorkflowID ||
		ticket.RunID != uuid.NewSHA1(scope.WorkflowID, []byte("run")) ||
		ticket.State != "queued" { return zero, false, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil { return zero, false, err }
	return ticket, true, nil
}
