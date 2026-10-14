-- +goose Up
-- SQL-side compression of the complete eligible queue. The result contains
-- one FIFO head per tenant/effective class and the maximum runnable charge.
-- This is a source for a future scoped affinity dispatcher; do not truncate
-- tenants or silently substitute this for the current full-candidate path.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_charge(p_estimated_ms bigint,p_unknown_seconds bigint)
RETURNS bigint LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_unknown_seconds IS NULL OR p_unknown_seconds<1 OR p_unknown_seconds>9007199254740991
    OR (p_estimated_ms IS NOT NULL AND
        (p_estimated_ms<0 OR p_estimated_ms>9007199254740991)) THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_CHARGE' USING ERRCODE='22023';
 END IF;
 IF p_estimated_ms IS NULL THEN RETURN p_unknown_seconds; END IF;
 RETURN greatest(1::bigint,p_estimated_ms/1000+
  CASE WHEN p_estimated_ms%1000=0 THEN 0 ELSE 1 END);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_charge(bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_charge(bigint,bigint) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_head_snapshot(
 p_pool text,p_now timestamptz,p_unknown_cost_seconds bigint,p_tenant_heavy_queued bigint)
RETURNS TABLE(task_id uuid,tenant_id uuid,original_queue text,created_at timestamptz,
 estimated_ms bigint,heavy boolean,tenant_queued_heavy bigint,effective_queue text,
 max_runnable_cost_seconds bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN ('cpu','gpu','signer') OR p_now IS NULL
    OR p_unknown_cost_seconds IS NULL OR p_unknown_cost_seconds<1
    OR p_unknown_cost_seconds>9007199254740991
    OR p_tenant_heavy_queued IS NULL OR p_tenant_heavy_queued<1
    OR p_tenant_heavy_queued>9007199254740991 THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_HEAD_SCOPE' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 WITH queued_heavy AS MATERIALIZED (
  SELECT queued.tenant_id,count(*) AS total FROM photo.tasks queued
  WHERE queued.resource_class='gpu' AND queued.state='queued'
  GROUP BY queued.tenant_id
 ), candidates AS (
  SELECT t.id AS task_id,t.tenant_id,r.config_snapshot->>'queue_class' AS original_queue,
         t.created_at,NULL::bigint AS estimated_ms,t.resource_class='gpu' AS heavy,
         coalesce(q.total,0::bigint) AS tenant_queued_heavy
  FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id
  JOIN photo.tenants owner ON owner.id=t.tenant_id AND owner.status='active'
  LEFT JOIN queued_heavy q ON q.tenant_id=t.tenant_id
  WHERE t.created_at<=p_now AND r.kind='v2'
   AND r.config_snapshot->>'queue_class' IN ('interactive','capture','batch','training')
   AND t.state='ready' AND t.attempt<6
   AND NOT t.cancel_requested AND NOT r.cancel_requested
   AND r.graph_sealed AND r.status IN ('queued','running')
   AND (t.admission_after IS NULL OR t.admission_after<=p_now)
   AND CASE WHEN p_pool='signer' THEN t.execution_profile->>'execution_pool'='signer'
    WHEN p_pool='gpu' THEN t.resource_class='gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer'
    ELSE t.resource_class<>'gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer' END
   AND EXISTS (SELECT 1 FROM photo.eligible_dispatch_nodes(t.id))
 ), classified AS MATERIALIZED (
  SELECT c.*,
   CASE WHEN p_now-c.created_at>=interval '10 minutes' THEN
    CASE c.original_queue WHEN 'training' THEN 'batch'
     WHEN 'batch' THEN 'capture' WHEN 'capture' THEN 'interactive'
     ELSE 'interactive' END
   ELSE c.original_queue END AS effective_queue,
   photo.capacity_queue_charge(c.estimated_ms,p_unknown_cost_seconds) AS cost_seconds
  FROM candidates c
  WHERE c.original_queue IN ('interactive','capture','batch','training')
   AND c.tenant_id IS NOT NULL AND c.task_id IS NOT NULL
 ), runnable AS MATERIALIZED (
  SELECT c.* FROM classified c
  WHERE NOT c.heavy OR c.tenant_queued_heavy<p_tenant_heavy_queued
 ), maximum AS (
  SELECT greatest(3600::bigint,coalesce(max(r.cost_seconds),0::bigint)) AS cap
  FROM runnable r
 ), heads AS (
  SELECT r.*,row_number() OVER (
   PARTITION BY r.tenant_id,r.effective_queue ORDER BY r.created_at,r.task_id) AS ordinal
  FROM runnable r
 )
 SELECT h.task_id,h.tenant_id,h.original_queue,h.created_at,h.estimated_ms,
        h.heavy,h.tenant_queued_heavy,h.effective_queue,m.cap
 FROM heads h CROSS JOIN maximum m WHERE h.ordinal=1
 ORDER BY h.tenant_id,h.effective_queue,h.created_at,h.task_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_head_snapshot(text,timestamptz,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_head_snapshot(text,timestamptz,bigint,bigint) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_queue_head_snapshot(text,timestamptz,bigint,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
