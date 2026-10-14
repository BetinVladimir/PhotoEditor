-- +goose Up
-- Exact installed policy bytes also constrain reports at the SQL boundary.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_budget_valid(p_policy jsonb,p_field text)
RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(CASE WHEN jsonb_typeof(p_policy->p_field)='number' THEN
  (p_policy->>p_field)::numeric BETWEEN 1 AND 9007199254740991
  AND trunc((p_policy->>p_field)::numeric)=(p_policy->>p_field)::numeric ELSE false END,false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_budget_valid(jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_budget_valid(jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
ALTER TABLE photo.capacity_policy_timings ADD CONSTRAINT capacity_policy_resource_budgets CHECK(
 photo.capacity_policy_budget_valid(policy_jcs::jsonb,'gpu_budget_bytes')
 AND photo.capacity_policy_budget_valid(policy_jcs::jsonb,'photo_ram_budget_bytes')
 AND photo.capacity_policy_budget_valid(policy_jcs::jsonb,'control_ram_budget_bytes'));
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text)'::regprocedure);
 needle:='accepted boolean;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected capacity policy declaration'; END IF;
 definition:=replace(definition,needle,'accepted boolean; installed_policy jsonb;');
 needle:='PERFORM policy_sha FROM photo.capacity_policy_timings WHERE policy_sha=p_policy_sha FOR SHARE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected capacity policy lookup'; END IF;
 definition:=replace(definition,needle,'SELECT policy_jcs::jsonb INTO installed_policy FROM photo.capacity_policy_timings WHERE policy_sha=p_policy_sha FOR SHARE;');
 needle:=' SELECT count(DISTINCT w.node_id),min(w.node_id) INTO nodes,node FROM photo.worker_registrations w';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected capacity report gate'; END IF;
 definition:=replace(definition,needle,' IF p_ram IS NULL OR p_ram NOT BETWEEN 0 AND 9007199254740991
 OR p_vram IS NULL OR p_vram NOT BETWEEN 0 AND 9007199254740991 THEN
 RAISE EXCEPTION ''INVALID_WORKER_REPORT'' USING ERRCODE=''22023''; END IF;
 p_ram:=least(p_ram,(installed_policy->>CASE WHEN p_class=''cpu'' THEN ''control_ram_budget_bytes'' ELSE ''photo_ram_budget_bytes'' END)::bigint);
 p_vram:=least(p_vram,(installed_policy->>''gpu_budget_bytes'')::bigint);
'||needle);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
