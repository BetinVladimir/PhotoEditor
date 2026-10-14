-- +goose Up
-- One current resident-model hint for a previously selected FIFO head.
-- The head remains the fallback. This function grants no reservation authority.
-- This tenant-scoped source repeats the exact eligibility predicates of 01633
-- while avoiding a full all-tenant scan for every affinity attempt.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_tenant_candidates(
 p_pool text,p_tenant uuid,p_now timestamptz)
RETURNS TABLE(task_id uuid,tenant_id uuid,original_queue text,created_at timestamptz,
 estimated_ms bigint,heavy boolean,tenant_queued_heavy bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN ('cpu','gpu','signer')
    OR p_tenant IS NULL OR p_now IS NULL THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_TENANT_SOURCE' USING ERRCODE='22023';
 END IF;
 RETURN QUERY WITH queued_heavy AS MATERIALIZED (
  SELECT count(*) AS total FROM photo.tasks queued WHERE queued.tenant_id=p_tenant
   AND queued.resource_class='gpu' AND queued.state='queued'
 ) SELECT t.id,t.tenant_id,r.config_snapshot->>'queue_class',t.created_at,
  NULL::bigint,t.resource_class='gpu',q.total
 FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id
 CROSS JOIN queued_heavy q
 JOIN photo.tenants owner ON owner.id=t.tenant_id AND owner.status='active'
 WHERE t.tenant_id=p_tenant AND t.created_at<=p_now
 AND r.kind='v2' AND r.config_snapshot->>'queue_class' IN ('interactive','capture','batch','training')
 AND t.state='ready' AND t.attempt<6 AND NOT t.cancel_requested AND NOT r.cancel_requested
 AND r.graph_sealed AND r.status IN ('queued','running')
 AND (t.admission_after IS NULL OR t.admission_after<=p_now)
 AND CASE WHEN p_pool='signer' THEN t.execution_profile->>'execution_pool'='signer'
 WHEN p_pool='gpu' THEN t.resource_class='gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer'
 ELSE t.resource_class<>'gpu' AND coalesce(t.execution_profile->>'execution_pool','')<>'signer' END
 AND EXISTS (SELECT 1 FROM photo.eligible_dispatch_nodes(t.id))
 ORDER BY t.created_at,t.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_tenant_candidates(text,uuid,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_tenant_candidates(text,uuid,timestamptz) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_head_affinity(
 p_pool text,p_head uuid,p_tenant uuid,p_class text,p_created timestamptz,
 p_now timestamptz,p_tenant_heavy_queued bigint,p_excluded uuid[])
RETURNS TABLE(task_id uuid,tenant_id uuid,original_queue text,created_at timestamptz,
 estimated_ms bigint,heavy boolean,tenant_queued_heavy bigint,
 model_lock_sha text,resident boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN ('cpu','gpu','signer')
    OR p_head IS NULL OR p_tenant IS NULL OR p_created IS NULL OR p_now IS NULL
    OR p_class IS NULL OR p_class NOT IN ('interactive','capture','batch','training')
    OR p_tenant_heavy_queued IS NULL OR p_tenant_heavy_queued<1
    OR p_tenant_heavy_queued>9007199254740991
    OR p_excluded IS NULL OR coalesce(array_length(p_excluded,1),0)>100
    OR (cardinality(p_excluded)>0 AND
        (array_ndims(p_excluded) IS DISTINCT FROM 1 OR
         array_lower(p_excluded,1) IS DISTINCT FROM 1))
    OR array_position(p_excluded,NULL::uuid) IS NOT NULL
    OR EXISTS (SELECT 1 FROM unnest(p_excluded) AS excluded(id)
               WHERE excluded.id='00000000-0000-0000-0000-000000000000'::uuid)
    OR (SELECT count(*)<>count(DISTINCT x.task_id) FROM unnest(p_excluded) AS x(task_id)) THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_AFFINITY_SCOPE' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 WITH candidates AS MATERIALIZED (
  SELECT c.task_id,c.tenant_id,c.original_queue,c.created_at,c.estimated_ms,
         c.heavy,c.tenant_queued_heavy,
         CASE WHEN p_now-c.created_at>=interval '10 minutes' THEN
          CASE c.original_queue WHEN 'training' THEN 'batch'
           WHEN 'batch' THEN 'capture' WHEN 'capture' THEN 'interactive'
           ELSE 'interactive' END
         ELSE c.original_queue END AS effective_queue
  FROM photo.capacity_queue_tenant_candidates(p_pool,p_tenant,p_now) c
 ), head AS MATERIALIZED (
  SELECT c.* FROM candidates c WHERE c.task_id=p_head AND c.tenant_id=p_tenant
   AND c.effective_queue=p_class AND c.created_at=p_created
   AND (NOT c.heavy OR c.tenant_queued_heavy<p_tenant_heavy_queued)
   AND NOT EXISTS (
    SELECT 1 FROM candidates earlier
    WHERE earlier.tenant_id=c.tenant_id AND earlier.effective_queue=c.effective_queue
     AND (NOT earlier.heavy OR earlier.tenant_queued_heavy<p_tenant_heavy_queued)
     AND (earlier.created_at,earlier.task_id)<(c.created_at,c.task_id)
   )
 ), resident_choice AS MATERIALIZED (
  SELECT c.task_id,c.tenant_id,c.original_queue,c.created_at,c.estimated_ms,
         c.heavy,c.tenant_queued_heavy,a.model_lock_sha,true AS resident
  FROM candidates c JOIN head h ON c.tenant_id=h.tenant_id
  CROSS JOIN LATERAL photo.capacity_task_affinity(ARRAY[c.task_id]) a
  WHERE c.effective_queue=p_class
   AND (NOT c.heavy OR c.tenant_queued_heavy<p_tenant_heavy_queued)
   AND (c.created_at,c.task_id)>=(h.created_at,h.task_id)
   AND c.created_at<=h.created_at+interval '30 seconds'
   AND c.task_id<>ALL(p_excluded)
   AND a.task_id=c.task_id AND a.resident
  ORDER BY c.created_at,c.task_id LIMIT 1
 )
 SELECT r.* FROM resident_choice r
 UNION ALL
 SELECT h.task_id,h.tenant_id,h.original_queue,h.created_at,h.estimated_ms,
        h.heavy,h.tenant_queued_heavy,NULL::text,false
 FROM head h WHERE NOT EXISTS (SELECT 1 FROM resident_choice);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_head_affinity(text,uuid,uuid,text,timestamptz,timestamptz,bigint,uuid[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_head_affinity(text,uuid,uuid,text,timestamptz,timestamptz,bigint,uuid[]) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_queue_head_affinity(text,uuid,uuid,text,timestamptz,timestamptz,bigint,uuid[]) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
