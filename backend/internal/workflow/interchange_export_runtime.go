package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validateInterchangeExportRuntimeSQL(ctx context.Context, db *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("interchange export runtime database unavailable")
	}
	var ready bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND count(*)=3 AND coalesce(bool_and(
  p.prosecdef AND owner.rolname='photo_fn_owner'
  AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
    OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
  AND CASE WHEN signatures.scheduler THEN
    has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
  ELSE has_function_privilege('photo_api',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE') END),false)
 FROM (VALUES
  ('photo.announce_v30_export_runtime(uuid,text)',true),
  ('photo.withdraw_v30_export_runtime(uuid)',true),
  ('photo.v30_export_runtime_ready(text)',false)) signatures(signature,scheduler)
 JOIN pg_proc p ON p.oid=to_regprocedure(signatures.signature)
 JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("interchange export runtime SQL unavailable")
	}
	return ctx.Err()
}

func interchangeExportLoopEligible(state *ExpansionLoopState) bool {
	return state != nil && state.Started.Load() && state.Ready.Load()
}

func maintainInterchangeExportRuntime(ctx context.Context, db *pgxpool.Pool,
	state *ExpansionLoopState, ready *atomic.Bool, instance uuid.UUID, bucket string) {
	if ready == nil {
		return
	}
	ready.Store(false)
	if db == nil || state == nil || instance == uuid.Nil || len(bucket) < 3 || len(bucket) > 63 {
		return
	}
	withdraw := func(parent context.Context) {
		bounded, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		_, _ = db.Exec(bounded, "SELECT photo.withdraw_v30_export_runtime($1)", instance)
	}
	defer func() {
		ready.Store(false)
		withdraw(context.Background())
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		if interchangeExportLoopEligible(state) {
			var accepted bool
			err := db.QueryRow(bounded,
				"SELECT photo.announce_v30_export_runtime($1,$2)", instance, bucket).Scan(&accepted)
			ready.Store(err == nil && accepted)
		} else {
			ready.Store(false)
			withdraw(bounded)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
