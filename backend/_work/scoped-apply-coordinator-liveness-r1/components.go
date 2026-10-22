package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/workflow"
	"sort"
	"time"
)

// Privileged observation is explicit and used only by this reviewed operator;
// all production mutating algorithms execute under photo_scheduler in the same
// transaction. It is never a replacement application role.
func operatorPool(ctx context.Context) (*pgxpool.Pool, error) {
	c, e := pgxpool.ParseConfig(os.Getenv("APPLY_OBSERVER_DATABASE_URL"))
	if e != nil {
		return nil, e
	}
	c.MaxConns = 1
	c.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	c.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	p, e := pgxpool.NewWithConfig(ctx, c)
	if e != nil {
		return nil, e
	}
	var a, b string
	if e = p.QueryRow(ctx, "SELECT current_user,session_user").Scan(&a, &b); e != nil || a != "rtx_snapshot_admin" || b != a {
		p.Close()
		return nil, errors.New("explicit observer principal required")
	}
	return p, nil
}
func observedTasks(ctx context.Context, tx pgx.Tx, s scope) error {
	var head int64
	if e := tx.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&head); e != nil || head != 2653 {
		return errors.New("installed head drift")
	}
	var total int
	if e := tx.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2", s.Tenant, s.Run).Scan(&total); e != nil || total != len(s.Tasks) {
		return errors.New("actual task readback stale")
	}
	for _, t := range s.Tasks {
		var tenant, run uuid.UUID
		var op, class string
		var raw []byte
		if e := tx.QueryRow(ctx, "SELECT tenant_id,run_id,operation,resource_class,execution_profile::text FROM photo.tasks WHERE id=$1", t.ID).Scan(&tenant, &run, &op, &class, &raw); e != nil {
			return e
		}
		// Readback hashes PostgreSQL jsonb::text bytes exactly, not a guessed Go JSON encoding.
		h := sha256.Sum256(raw)
		if tenant != s.Tenant || run != s.Run || op != t.Operation || class != t.Class || hex.EncodeToString(h[:]) != t.ProfileSHA {
			return errors.New("actual task/profile readback mismatch")
		}
	}
	return nil
}

// One normal component, one caller-owned transaction. Unknown outside effects
// conservatively refuse and roll back; no global scheduler activation or reset.
func componentOne(ctx context.Context, p *pgxpool.Pool, s scope, component string) (count int, err error) {
	if e := s.validate(time.Now().UTC()); e != nil {
		return 0, e
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(context.Background())
	if e = observedTasks(ctx, tx, s); e != nil {
		return 0, e
	}
	deferBefore, e := s.deferRows(ctx, tx)
	if e != nil {
		return 0, e
	}
	started := time.Now().UTC()
	selected := map[string]bool{}
	before, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
		return 0, e
	}
	switch component {
	case "dispatch":
		count, e = dispatchBounded(s, selected, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
			return (workflow.Dispatcher{}).DispatchOneInTransaction(ctx, tx, accept)
		})
	case "recover":
		count, e = (workflow.Recovery{DB: tx}).Recover(ctx, 1)
	default:
		return 0, errors.New("closed component")
	}
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
		return 0, e
	}
	if e = observedTasks(ctx, tx, s); e != nil {
		return 0, e
	}
	deferAfter, e := s.deferRows(ctx, tx)
	if e != nil {
		return 0, e
	}
	deferSelected := map[string]bool{}
	for id := range selected {
		if _, ok := s.deferred[id]; ok {
			deferSelected[id] = true
		}
	}
	if e = validateDeferrals(deferBefore, deferAfter, deferSelected, started, time.Now().UTC()); e != nil {
		return 0, e
	}
	if s.proof != nil {
		ids := make([]string, 0, len(deferSelected))
		for id := range deferSelected {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		*s.proof = deferralReadback{Selected: ids, Before: deferBefore, After: deferAfter, Completion: false}
	}
	after, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if !equalSnapshots(before, after) {
		return 0, errors.New("outside component effect refused")
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return count, nil
}
