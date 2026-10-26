package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/workflow"
)

// Explicit invocation only. The normal algorithm may select globally; the same
// caller transaction rolls back every outside-table effect. Never loops/retries.
func recoverArchives(ctx context.Context, input Scope) error {
	s := scope{Tenant: input.Jobs[0].Tenant, Tasks: input.Tasks, Nodes: input.Nodes, Jobs: input.Jobs}
	if e := s.validate(); e != nil {
		return e
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("DOWNLOAD_OBSERVER_DATABASE_URL"))
	if e != nil {
		return errors.New("observer unavailable")
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return e
	}
	defer pool.Close()
	var current, session string
	if e = pool.QueryRow(ctx, "SELECT current_user,session_user").Scan(&current, &session); e != nil || current != "rtx_snapshot_admin" || session != current {
		return errors.New("explicit observer required")
	}
	n, e := componentWithBegin(ctx, pool.Begin, func(tx pgx.Tx) (int, error) {
		var head int64
		if e := tx.QueryRow(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil || requireHead(head) != nil {
			return 0, errors.New("head drift")
		}
		if input.RecoveryPhase != "plan" && input.RecoveryPhase != "activate" {
			return 0, errors.New("explicit recovery phase required")
		}
		old, dbNow, e := recoveryTask(ctx, tx)
		if e != nil {
			return 0, e
		}
		if e = recoveryBefore(old, input.RecoveryPhase, dbNow); e != nil {
			return 0, e
		}
		strict := scope{recoveryPhase: input.RecoveryPhase}
		before, e := capture(ctx, tx, strict)
		if e != nil {
			return 0, e
		}
		if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
			return 0, e
		}
		count, e := (workflow.Recovery{DB: tx}).Recover(ctx, 1)
		if e != nil {
			return 0, e
		}
		if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
			return 0, e
		}
		newTask, _, e := recoveryTask(ctx, tx)
		if e != nil {
			return 0, e
		}
		if e = validateRecoveryEffect(old, newTask, input.RecoveryPhase, count, dbNow); e != nil {
			return 0, e
		}
		after, e := capture(ctx, tx, strict)
		if e != nil {
			return 0, e
		}
		if !equalSnapshots(before, after) {
			return 0, errors.New("outside normal archive recovery effect refused")
		}
		return count, nil
	})
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ONE_NORMAL_RECOVERY_REQUIRES_READBACK_NOT_DISPATCH", "count": n, "recovery_phase": input.RecoveryPhase})
}
