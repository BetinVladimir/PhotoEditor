package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeliverySigningLeasePin struct {
	Tenant uuid.UUID `json:"tenant_id"`
	Policy uuid.UUID `json:"policy_id"`
	SHA    string    `json:"policy_sha"`
}

func validateDeliveryRuntimeSQL(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return errors.New("delivery runtime database unavailable")
	}
	var installed bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND count(*)=2
 AND coalesce(bool_and(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND ('search_path=pg_catalog,photo'=ANY(p.proconfig) OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.privilege_type='EXECUTE' AND acl.grantee NOT IN(p.proowner,'photo_scheduler'::regrole::oid))),false)
 FROM (VALUES ('photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)'::regprocedure),
 ('photo.withdraw_v30_delivery_runtime(uuid)'::regprocedure)) signatures(oid)
 JOIN pg_proc p ON p.oid=signatures.oid JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&installed)
	if err != nil || !installed {
		return errors.New("delivery runtime SQL unavailable")
	}
	return ctx.Err()
}

func maintainDeliveryRuntime(ctx context.Context, db *pgxpool.Pool, state *ExpansionLoopState,
	ready *atomic.Bool, instance uuid.UUID, bucket string, signingTenants []uuid.UUID, signingPins []DeliverySigningLeasePin) {
	if ready == nil {
		return
	}
	ready.Store(false)
	if db == nil || state == nil || instance == uuid.Nil || len(bucket) < 3 || len(bucket) > 63 {
		return
	}
	pinsJSON, err := json.Marshal(signingPins)
	if err != nil || len(signingPins) > 4096 {
		return
	}
	withdraw := func(parent context.Context) {
		bounded, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		_, _ = db.Exec(bounded, `SELECT photo.withdraw_v30_delivery_runtime($1)`, instance)
	}
	defer func() { ready.Store(false); withdraw(context.Background()) }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		if state.Started.Load() && (state.Ready.Load() || (state.Failures.Load() == 0 && state.Materialized.Load() == 0)) {
			var accepted bool
			err := db.QueryRow(bounded, `SELECT photo.announce_v30_delivery_runtime_pinned($1,$2,$3,$4::jsonb)`, instance, bucket, signingTenants, string(pinsJSON)).Scan(&accepted)
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
