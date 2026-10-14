package v2region

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// SealInTransaction rechecks persisted node and live authority before registering
// the already-published closure. Caller owns TX rollback/commit and tenant scope.
// There is no object IO; retries reopen the same node-owned input inventory.
func (n FrozenRegionNode) SealInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, key string, snapshot uuid.UUID, policySHA string, reservedBytes int64, built v2input.Built) error {
	if tx == nil {
		return ErrRegionInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	shell, sources, err := n.DraftShell(who, key, snapshot, policySHA, reservedBytes)
	if err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRow(ctx, "SELECT photo.v26_region_seal_scope_scheduler($1,$2,$3,$4,$5)", shell.Tenant, shell.Run, who.UserID, who.SessionID, json.RawMessage(n.JSON())).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRegionInvalid
	}
	return v2input.SealGraphInTransaction(ctx, tx, shell.Tenant, shell.Run, []v2input.GraphTask{{ID: sources[0].ID, Built: built, Open: sources[0].Open, Direct: sources[0].Direct}})
}
