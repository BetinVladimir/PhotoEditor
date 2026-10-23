package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

// FailTerminalPredecessor uses the normal current-lease, saved-stage SQL gate.
// It never decides a retry, edits a task or bypasses the public billing wrapper.
func (s ApplyStore) FailTerminalPredecessor(ctx context.Context, l ApplyLease) (bool, error) {
	if s.DB == nil || l.TenantID == uuid.Nil || l.RunID == uuid.Nil || l.Token == uuid.Nil || l.GraphVersion != 4 {
		return false, errors.New("invalid apply terminal failure authority")
	}
	var failed bool
	err := s.DB.QueryRow(ctx, `SELECT photo.fail_apply_terminal_stage($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&failed)
	return failed, err
}

// The isolated measurement ledger has no database task-failure model. Its Claim
// refuses a failed ledger and waits for actual measured outputs; it must not
// invent a SQL terminal failure or qualification result to satisfy this port.
func (s *OperatorApplyLedger) FailTerminalPredecessor(ctx context.Context, l ApplyLease) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s == nil || !s.scope(l) || l.GraphVersion != 4 || l.Phase != s.lease.Phase || l.ItemCount != s.lease.ItemCount || l.SourceCollectionID != s.lease.SourceCollectionID || (l.StyleVersionID == nil) != (s.lease.StyleVersionID == nil) || (l.StyleVersionID != nil && s.lease.StyleVersionID != nil && *l.StyleVersionID != *s.lease.StyleVersionID) || l.StageIndex == nil || s.lease.StageIndex == nil || *l.StageIndex != *s.lease.StageIndex {
		return false, errors.New("operator terminal failure scope invalid")
	}
	return false, nil
}
