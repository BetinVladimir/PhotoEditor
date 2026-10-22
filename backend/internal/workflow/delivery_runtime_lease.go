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
	Tenant    uuid.UUID `json:"tenant_id"`
	Policy    uuid.UUID `json:"policy_id"`
	SHA       string    `json:"policy_sha"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
}

func validateDeliveryRuntimeSQL(ctx context.Context, db *pgxpool.Pool) error {
	if db == nil {
		return errors.New("delivery runtime database unavailable")
	}
	var installed bool
	err := db.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND count(*)=6
 AND NOT has_function_privilege('photo_scheduler',
  'photo.claim_v30_delivery_expansion()'::regprocedure,'EXECUTE')
 AND coalesce(bool_and(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND ('search_path=pg_catalog,photo'=ANY(p.proconfig) OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.privilege_type='EXECUTE' AND acl.grantee NOT IN(p.proowner,'photo_scheduler'::regrole::oid))),false)
 FROM (VALUES ('photo.announce_v30_delivery_runtime_complete(uuid,text,uuid[],jsonb,boolean)'::regprocedure),
 ('photo.withdraw_v30_delivery_runtime(uuid)'::regprocedure),
 ('photo.claim_v30_delivery_expansion_bound(uuid,text)'::regprocedure),
 ('photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)'::regprocedure),
 ('photo.fail_next_v30_delivery_missing_source(uuid,text)'::regprocedure),
 ('photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean)'::regprocedure)) signatures(oid)
 JOIN pg_proc p ON p.oid=signatures.oid JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&installed)
	if err != nil || !installed {
		return errors.New("delivery runtime SQL unavailable")
	}
	err = db.QueryRow(ctx, `SELECT count(*)=3 AND coalesce(bool_and(
  p.prosecdef AND owner.rolname='photo_fn_owner'
  AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
    OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
  AND has_function_privilege('photo_api',p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.privilege_type='EXECUTE'
      AND acl.grantee NOT IN (p.proowner,'photo_api'::regrole::oid))),false)
 FROM (VALUES
  (to_regprocedure('photo.v30_delivery_runtime_ready(text)')),
  (to_regprocedure('photo.v30_delivery_metadata_runtime_ready(text)')),
  (to_regprocedure('photo.v30_delivery_signing_runtime_ready(text,uuid,uuid,text)'))
 ) signatures(oid) JOIN pg_proc p ON p.oid=signatures.oid
 JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&installed)
	if err != nil || !installed {
		return errors.New("delivery admission SQL unavailable")
	}
	err = db.QueryRow(ctx, `SELECT current_user='photo_scheduler'
 AND count(*)=1 AND coalesce(bool_and(p.prosecdef AND owner.rolname='photo_fn_owner'
  AND ('search_path=pg_catalog,photo'=ANY(p.proconfig)
   OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
  AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
  AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')),false)
 AND position('photo.v30_delivery_expansion_bucket_ready(' IN
  pg_get_functiondef('photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)'::regprocedure))>0
 AND position('photo.v30_delivery_expansion_bucket_ready(' IN
  pg_get_functiondef('photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)'::regprocedure))>0
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
 WHERE p.oid=to_regprocedure('photo.v30_delivery_expansion_bucket_ready(text,uuid)')`).Scan(&installed)
	if err != nil || !installed {
		return errors.New("delivery source bucket SQL unavailable")
	}
	return ctx.Err()
}

func maintainDeliveryRuntime(ctx context.Context, db *pgxpool.Pool, state *ExpansionLoopState,
	metadataState *ExpansionLoopState, ready *atomic.Bool, instance uuid.UUID, bucket string, signingTenants []uuid.UUID, signingPins []DeliverySigningLeasePin) {
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
		if deliveryLoopReady(state) {
			var accepted bool
			metadataReady := metadataState != nil && metadataState.Ready.Load()
			err := db.QueryRow(bounded, `SELECT photo.announce_v30_delivery_runtime_complete($1,$2,$3,$4::jsonb,$5)`, instance, bucket, signingTenants, string(pinsJSON), metadataReady).Scan(&accepted)
			healthy := err == nil && accepted && deliveryLoopReady(state)
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

// A started loop has not necessarily completed its first pass. Advertise
// admission only after ProcessOne has returned successfully.
func deliveryLoopReady(state *ExpansionLoopState) bool {
	return state != nil && state.Started.Load() && state.Ready.Load()
}
