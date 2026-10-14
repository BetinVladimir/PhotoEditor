-- +goose Up
-- Return one DRR-eligible FIFO head and the complete runnable charge cap.
-- All tenant/class heads are considered inside SQL; no application-side page
-- limit may silently remove a tenant from the fair selection. The materialized
-- query can spill to PostgreSQL temporary storage under work_mem.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_queue_bounded_head_choice(
 p_pool text,p_now timestamptz,p_unknown_cost_seconds bigint,
 p_tenant_heavy_queued bigint,p_last_tenant uuid,p_class_cursor integer,
 p_deficits bigint[],p_excluded_heads uuid[])
RETURNS TABLE(task_id uuid,tenant_id uuid,original_queue text,created_at timestamptz,
 estimated_ms bigint,heavy boolean,tenant_queued_heavy bigint,effective_queue text,
 max_runnable_cost_seconds bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN ('cpu','gpu','signer') OR p_now IS NULL
    OR p_unknown_cost_seconds IS NULL OR p_unknown_cost_seconds NOT BETWEEN 1 AND 9007199254740991
    OR p_tenant_heavy_queued IS NULL OR p_tenant_heavy_queued NOT BETWEEN 1 AND 9007199254740991
    OR p_last_tenant IS NULL OR p_class_cursor IS NULL OR p_class_cursor NOT BETWEEN 0 AND 3
    OR p_deficits IS NULL OR array_ndims(p_deficits) IS DISTINCT FROM 1
    OR array_lower(p_deficits,1) IS DISTINCT FROM 1
    OR cardinality(p_deficits)<>4
    OR EXISTS(SELECT 1 FROM unnest(p_deficits) AS deficit(value)
              WHERE deficit.value IS NULL OR deficit.value<0 OR deficit.value>9007199254740991)
    OR p_excluded_heads IS NULL OR cardinality(p_excluded_heads)>100
    OR (cardinality(p_excluded_heads)>0 AND
        (array_ndims(p_excluded_heads) IS DISTINCT FROM 1 OR
         array_lower(p_excluded_heads,1) IS DISTINCT FROM 1))
    OR array_position(p_excluded_heads,NULL::uuid) IS NOT NULL
    OR EXISTS(SELECT 1 FROM unnest(p_excluded_heads) AS excluded(id)
              WHERE excluded.id='00000000-0000-0000-0000-000000000000'::uuid)
    OR (SELECT count(*)<>count(DISTINCT excluded.id)
        FROM unnest(p_excluded_heads) AS excluded(id)) THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_BOUNDED_HEAD_SCOPE' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 WITH heads AS MATERIALIZED (
  SELECT h.* FROM photo.capacity_queue_head_snapshot(
   p_pool,p_now,p_unknown_cost_seconds,p_tenant_heavy_queued) h
 ), cap AS (
  SELECT greatest(3600::bigint,coalesce(max(h.max_runnable_cost_seconds),0::bigint)) AS value
  FROM heads h
 )
 SELECT chosen.task_id,chosen.tenant_id,chosen.original_queue,chosen.created_at,
        chosen.estimated_ms,chosen.heavy,chosen.tenant_queued_heavy,
        chosen.effective_queue,cap.value
 FROM cap LEFT JOIN LATERAL (
  SELECT h.* FROM heads h
  WHERE h.task_id<>ALL(p_excluded_heads)
   AND photo.capacity_queue_charge(h.estimated_ms,p_unknown_cost_seconds)<=
    p_deficits[array_position(ARRAY['interactive','capture','batch','training']::text[],h.effective_queue)]
  ORDER BY (array_position(ARRAY['interactive','capture','batch','training']::text[],h.effective_queue)
              -1-p_class_cursor+4)%4,
           CASE WHEN h.tenant_id>p_last_tenant THEN 0 ELSE 1 END,
           h.tenant_id,h.created_at,h.task_id
  LIMIT 1
 ) chosen ON true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_queue_bounded_head_choice(text,timestamptz,bigint,bigint,uuid,integer,bigint[],uuid[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_queue_bounded_head_choice(text,timestamptz,bigint,bigint,uuid,integer,bigint[],uuid[]) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_queue_bounded_head_choice(text,timestamptz,bigint,bigint,uuid,integer,bigint[],uuid[]) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
