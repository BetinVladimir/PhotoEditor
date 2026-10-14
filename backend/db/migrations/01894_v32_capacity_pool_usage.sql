-- +goose Up
-- Aggregate only pinned, fresh operator registrations. Signer and general
-- workers sharing a CPU node cannot supply a pool-specific denominator.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_pool_resource_usage(p_pool text,p_policy_sha text)
RETURNS TABLE(used_reservations bigint,available_slots bigint,node_count bigint,observed_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy jsonb; per_node bigint; members text[]; total_nodes bigint;
 used bigint; allowed numeric; observed timestamptz;
BEGIN
 IF p_pool IS NULL OR p_pool NOT IN('cpu','gpu','signer') OR
  p_policy_sha IS NULL OR p_policy_sha !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_USAGE_SCOPE' USING ERRCODE='22023';
 END IF;
 SELECT policy_jcs::jsonb INTO policy FROM photo.capacity_policy_timings WHERE policy_sha=p_policy_sha;
 IF policy IS NULL THEN RAISE EXCEPTION 'CAPACITY_POLICY_UNAVAILABLE'; END IF;
 per_node:=(policy->>CASE WHEN p_pool='gpu' THEN 'gpu_concurrency' ELSE 'cpu_concurrency' END)::bigint;
 IF per_node IS NULL OR per_node<1 THEN RAISE EXCEPTION 'INVALID_CAPACITY_POLICY'; END IF;
 SELECT array_agg(n.node_id ORDER BY n.node_id),count(*) INTO members,total_nodes
 FROM photo.resource_nodes n WHERE n.enabled AND n.capacity_report_policy_sha=p_policy_sha
 AND photo.capacity_policy_node_fresh(n) AND EXISTS(
  SELECT 1 FROM photo.worker_registrations w WHERE w.node_id=n.node_id AND w.enabled
  AND w.capacity_policy_sha=p_policy_sha AND photo.capacity_policy_worker_fresh(w)
  AND CASE WHEN p_pool='signer' THEN w.execution_pool='signer' AND w.resource_classes ? 'cpu'
   AND w.signer_qualified_until>clock_timestamp()
   WHEN p_pool='gpu' THEN w.execution_pool='general' AND w.resource_classes ? 'gpu'
   ELSE w.execution_pool='general' AND w.resource_classes ?| ARRAY['cpu','media','archive'] END);
 IF total_nodes>10000 THEN RAISE EXCEPTION 'CAPACITY_USAGE_NODE_LIMIT'; END IF;
 IF p_pool IN('cpu','signer') AND EXISTS(
  SELECT 1 FROM photo.worker_registrations w WHERE w.node_id=ANY(members) AND w.enabled
   AND w.capacity_policy_sha=p_policy_sha AND photo.capacity_policy_worker_fresh(w)
   AND w.resource_classes ?| ARRAY['cpu','media','archive']
  GROUP BY w.node_id HAVING count(DISTINCT w.execution_pool)>1) THEN
  RAISE EXCEPTION 'MIXED_SIGNER_GENERAL_NODE';
 END IF;
 allowed:=total_nodes::numeric*per_node::numeric;
 IF allowed>9007199254740991 THEN RAISE EXCEPTION 'CAPACITY_USAGE_OVERFLOW'; END IF;
 SELECT count(*) INTO used FROM photo.resource_reservations rr
 JOIN photo.tasks t ON t.tenant_id=rr.tenant_id AND t.id=rr.task_id
 WHERE rr.node_id=ANY(members) AND rr.state='reserved'
 AND CASE WHEN p_pool='gpu' THEN t.resource_class='gpu' ELSE t.resource_class<>'gpu' END;
 observed:=clock_timestamp();
 RETURN QUERY SELECT used,allowed::bigint,total_nodes,observed;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_pool_resource_usage(text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_pool_resource_usage(text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.capacity_pool_resource_usage(text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
