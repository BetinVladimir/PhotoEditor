package v2region

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// BeginInTransaction proves live scope and binds idempotency in the same TX.
// Caller owns rollback/commit and must set app.tenant_id. No object IO occurs.
// This creates a blocked draft only; closure registration/sealing is separate.
func (n FrozenRegionNode) BeginInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, key string, snapshot uuid.UUID, policySHA string, reservedBytes int64, requestSHA string) (string, uuid.UUID, error) {
	if tx == nil || !validRegionSHA(requestSHA) {
		return "", uuid.Nil, ErrRegionInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", uuid.Nil, err
	}
	shell, _, err := n.DraftShell(who, key, snapshot, policySHA, reservedBytes)
	if err != nil {
		return "", uuid.Nil, err
	}
	var node regionNodeDocument
	if json.Unmarshal(n.body, &node) != nil {
		return "", uuid.Nil, ErrRegionInvalid
	}
	var allowed bool
	err = tx.QueryRow(ctx, "SELECT photo.v26_region_admission_scope_scheduler($1,$2,$3,$4,$5)", shell.Tenant, snapshot, who.UserID, who.SessionID, json.RawMessage(node.ParamsJCS)).Scan(&allowed)
	if err != nil {
		return "", uuid.Nil, err
	}
	if !allowed {
		return "", uuid.Nil, ErrRegionInvalid
	}
	return v2input.BeginAdmissionInTransaction(ctx, tx, shell, key, requestSHA)
}
