package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validateInterchangeImportRuntimeSQL(ctx context.Context, db *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("interchange import runtime database unavailable")
	}
	var ready bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND
  coalesce(bool_and(p.prosecdef AND owner.rolname='photo_fn_owner'
    AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
      OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
    AND has_function_privilege(current_user,p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_maintenance',p.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
      WHERE acl.privilege_type='EXECUTE'
        AND acl.grantee NOT IN (p.proowner,'photo_scheduler'::regrole::oid))),false)
  AND count(*)=2 FROM (VALUES
	    ('photo.announce_v30_import_runtime_bound(uuid,text)'::regprocedure),
    ('photo.withdraw_v30_import_runtime(uuid)'::regprocedure)) signatures(oid)
  JOIN pg_proc p ON p.oid=signatures.oid
  JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("interchange import runtime SQL unavailable")
	}
	return ctx.Err()
}

// The lease is advertised only while an installed import loop is healthy.
// Its independent ticker keeps availability visible during bounded object IO.
func interchangeImportLoopEligible(state *ExpansionLoopState) bool {
	return state != nil && state.Started.Load() && (state.Ready.Load() ||
		(state.Failures.Load() == 0 && state.Materialized.Load() == 0))
}

func maintainInterchangeImportRuntime(ctx context.Context, db *pgxpool.Pool,
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
		_, _ = db.Exec(bounded, "SELECT photo.withdraw_v30_import_runtime($1)", instance)
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
		if interchangeImportLoopEligible(state) {
			var accepted bool
			err := db.QueryRow(bounded,
				"SELECT photo.announce_v30_import_runtime_bound($1,$2)", instance, bucket).Scan(&accepted)
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
