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
	err := db.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND count(*)=4 AND coalesce(bool_and(
  p.prosecdef AND owner.rolname='photo_fn_owner'
  AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
    OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.privilege_type='EXECUTE' AND acl.grantee NOT IN
      (p.proowner,CASE WHEN signatures.scheduler THEN 'photo_scheduler'::regrole::oid
       ELSE 'photo_api'::regrole::oid END))
  AND CASE WHEN signatures.scheduler THEN
    has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
  ELSE has_function_privilege('photo_api',p.oid,'EXECUTE')
    AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE') END),false)
 FROM (VALUES
  ('photo.announce_v30_export_runtime(uuid,text)',true),
  ('photo.withdraw_v30_export_runtime(uuid)',true),
  ('photo.v30_export_runtime_ready(text)',false),
  ('photo.v30_export_pending_runtime_ready(uuid,uuid,uuid,text,text)',false)) signatures(signature,scheduler)
 JOIN pg_proc p ON p.oid=to_regprocedure(signatures.signature)
 JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("interchange export runtime SQL unavailable")
	}
	err = db.QueryRow(ctx, `SELECT c.relkind='r' AND c.relrowsecurity AND c.relforcerowsecurity
 AND owner.rolname='photo_runtime_owner'
 AND NOT owner.rolcanlogin AND NOT owner.rolsuper AND NOT owner.rolbypassrls AND NOT owner.rolinherit
 AND has_table_privilege('photo_fn_owner',c.oid,'SELECT')
 AND has_table_privilege('photo_fn_owner',c.oid,'INSERT')
 AND has_table_privilege('photo_fn_owner',c.oid,'UPDATE')
 AND NOT has_table_privilege('photo_fn_owner',c.oid,'DELETE')
 AND EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid
  AND policy.polname='trusted_functions' AND policy.polcmd='*'
  AND policy.polroles=ARRAY['photo_fn_owner'::regrole::oid]
  AND pg_get_expr(policy.polqual,policy.polrelid)='true'
  AND pg_get_expr(policy.polwithcheck,policy.polrelid)='true')
 AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker',
  'photo_scheduler','photo_gateway','photo_portal','photo_maintenance']) role(name)
  WHERE has_table_privilege(role.name,c.oid,'SELECT')
   OR has_table_privilege(role.name,c.oid,'INSERT')
   OR has_table_privilege(role.name,c.oid,'UPDATE')
   OR has_table_privilege(role.name,c.oid,'DELETE'))
 FROM pg_class c JOIN pg_roles owner ON owner.oid=c.relowner
 WHERE c.oid=to_regclass('photo.v30_export_runtime_leases')`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("interchange export runtime lease table authority unavailable")
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
			healthy := err == nil && accepted && interchangeExportLoopEligible(state)
			ready.Store(healthy)
			if !healthy {
				withdraw(context.WithoutCancel(ctx))
			}
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
