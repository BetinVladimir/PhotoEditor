package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var capacitySchedulerSignatures = []string{
	"photo.capacity_scheduler_policy(text)",
	"photo.capacity_queue_candidates(text)",
	"photo.capacity_queue_candidate(text,uuid)",
	"photo.capacity_pick_selected_task(uuid,uuid,text,bigint)",
	"photo.capacity_heavy_admission_available(text)",
	"photo.capacity_affinity_dispatch_nodes(uuid)",
	"photo.capacity_task_affinity(uuid[])",
	"photo.capacity_queue_head_snapshot(text,timestamptz,bigint,bigint)",
	"photo.capacity_queue_head_affinity(text,uuid,uuid,text,timestamptz,timestamptz,bigint,uuid[])",
	"photo.capacity_queue_bounded_head_choice(text,timestamptz,bigint,bigint,uuid,integer,bigint[],uuid[])",
	"photo.begin_capacity_node_drain(text)",
	"photo.capacity_node_drain_status(text,bigint)",
	"photo.reap_capacity_node_drain_task(text,bigint)",
	"photo.capacity_pool_pending_backlog(text)",
	"photo.capacity_pool_resource_usage(text,text)",
}
var capacitySchedulerPrivateSignatures = []string{
	"photo.capacity_queue_charge(bigint,bigint)",
	"photo.capacity_queue_tenant_candidates(text,uuid,timestamptz)",
}

const capacitySchedulerContractSQL = `SELECT coalesce(bool_and(coalesce(
 p.oid IS NOT NULL AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
 AND has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 ,false)),false) AND (
 SELECT coalesce(bool_and(coalesce(
  q.oid IS NOT NULL AND q.prosecdef AND pg_get_userbyid(q.proowner)='photo_fn_owner'
  AND q.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
  AND NOT has_function_privilege('photo_scheduler',q.oid,'EXECUTE')
  AND NOT has_function_privilege('photo_api',q.oid,'EXECUTE')
  AND NOT has_function_privilege('photo_worker',q.oid,'EXECUTE')
 ,false)),false) FROM unnest($2::text[]) private_signature
 LEFT JOIN pg_proc q ON q.oid=to_regprocedure(private_signature)
 ) FROM unnest($1::text[]) signature
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)`

// CheckCapacitySchedulerContract refuses partial migrations before any pool
// starts dispatching. Function existence alone is insufficient authority.
func CheckCapacitySchedulerContract(ctx context.Context, tx pgx.Tx) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return errors.New("missing capacity scheduler contract transaction")
	}
	signatures := append([]string(nil), capacitySchedulerSignatures...)
	privateSignatures := append([]string(nil), capacitySchedulerPrivateSignatures...)
	var ready bool
	if err := tx.QueryRow(ctx, capacitySchedulerContractSQL, signatures, privateSignatures).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return errors.New("capacity scheduler SQL contract unavailable")
	}
	return nil
}
