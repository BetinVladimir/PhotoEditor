-- +goose Up
-- Exact selected-task eligibility, avoiding a full-backlog scan per reservation.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_candidate(p_pool text,p_task uuid)
RETURNS TABLE(task_id uuid,tenant_id uuid,original_queue text,created_at timestamptz,
 estimated_ms bigint,heavy boolean,tenant_queued_heavy bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_task IS NULL THEN RAISE EXCEPTION 'INVALID_CAPACITY_TASK' USING ERRCODE='22023'; END IF;
 IF p_pool IS NULL OR p_pool NOT IN('cpu','gpu','signer') THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_POOL' USING ERRCODE='22023'; END IF;
 RETURN QUERY SELECT t.id,t.tenant_id,r.config_snapshot->>'queue_class',t.created_at,
 NULL::bigint,t.resource_class='gpu',
 (SELECT count(*) FROM photo.tasks queued WHERE queued.tenant_id=t.tenant_id
   AND queued.resource_class='gpu' AND queued.state='queued')
 FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id
 JOIN photo.tenants owner ON owner.id=t.tenant_id AND owner.status='active'
 WHERE t.id=p_task AND r.kind='v2' AND r.config_snapshot->>'queue_class' IN('interactive','capture','batch','training')
 AND t.state='ready' AND t.attempt<6 AND NOT t.cancel_requested AND NOT r.cancel_requested
 AND r.graph_sealed AND r.status IN('queued','running')
 AND (t.admission_after IS NULL OR t.admission_after<=clock_timestamp())
 AND CASE WHEN p_pool='signer' THEN t.execution_profile->>'execution_pool'='signer'
 WHEN p_pool='gpu' THEN t.resource_class='gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer'
 ELSE t.resource_class<>'gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer' END
 AND EXISTS(SELECT 1 FROM photo.eligible_dispatch_nodes(t.id))
 ORDER BY t.tenant_id,t.created_at,t.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_candidate(text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_candidate(text,uuid) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_queue_candidate(text,uuid) TO photo_scheduler;
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:='photo.capacity_queue_candidates(p_pool)';
BEGIN
 definition:=pg_get_functiondef('photo.capacity_pick_selected_task(uuid,uuid,text,bigint)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected selected capacity candidate call'; END IF;
 EXECUTE replace(definition,needle,'photo.capacity_queue_candidate(p_pool,p_task)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
