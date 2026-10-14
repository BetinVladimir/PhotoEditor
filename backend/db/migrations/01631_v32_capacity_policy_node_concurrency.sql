-- +goose Up
ALTER TABLE photo.capacity_policy_timings ADD CONSTRAINT capacity_policy_concurrency_values CHECK(
 photo.capacity_policy_budget_valid(policy_jcs::jsonb,'gpu_concurrency')
 AND photo.capacity_policy_budget_valid(policy_jcs::jsonb,'cpu_concurrency'));
-- A node lock held by reserve_dispatch_resources serializes this count and insert.
-- CPU/media/archive compete for the same physical CPU task budget across tenants.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_node_concurrency_available(p_node photo.resource_nodes,p_class text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH budget AS (
  SELECT CASE WHEN p_node.capacity_report_policy_sha IS NULL THEN
   CASE WHEN p_class='gpu' THEN 1::bigint ELSE 9007199254740991::bigint END
  ELSE (c.policy_jcs::jsonb->>CASE WHEN p_class='gpu' THEN 'gpu_concurrency' ELSE 'cpu_concurrency' END)::bigint END AS allowed
  FROM (SELECT 1) seed LEFT JOIN photo.capacity_policy_timings c ON c.policy_sha=p_node.capacity_report_policy_sha
 ), occupied AS (
  SELECT count(*) AS used FROM photo.resource_reservations rr
  JOIN photo.tasks t ON t.tenant_id=rr.tenant_id AND t.id=rr.task_id
  WHERE rr.node_id=p_node.node_id AND rr.state='reserved' AND rr.expires_at>clock_timestamp()
  AND CASE WHEN p_class='gpu' THEN t.resource_class='gpu' ELSE t.resource_class<>'gpu' END
 )
 SELECT coalesce(p_class IN('cpu','media','archive','gpu') AND occupied.used<budget.allowed,false) FROM budget,occupied;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_node_concurrency_available(photo.resource_nodes,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_node_concurrency_available(photo.resource_nodes,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- Preserve resource estimates, fairness, model and signer identity gates.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure);
 needle:='(t.resource_class=''gpu'' AND used_gpu>=1)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected capacity concurrency reserve definition'; END IF;
 EXECUTE replace(definition,needle,'NOT photo.capacity_policy_node_concurrency_available(n,t.resource_class)');
 definition:=pg_get_functiondef('photo.eligible_dispatch_nodes(uuid)'::regprocedure);
 needle:='WHERE n.enabled AND ';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected capacity concurrency selection definition'; END IF;
 EXECUTE replace(definition,needle,'WHERE n.enabled AND photo.capacity_policy_node_concurrency_available(n,t.resource_class) AND ');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
