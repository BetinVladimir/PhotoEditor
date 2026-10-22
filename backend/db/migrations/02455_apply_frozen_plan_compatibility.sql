-- +goose Up
-- Accepted snapshots and registered stages are never rewritten in place.
ALTER TABLE photo.apply_builds ADD COLUMN plan_contract_version int NOT NULL DEFAULT 1 CHECK(plan_contract_version IN(1,2));
ALTER TABLE photo.apply_builds ALTER COLUMN plan_contract_version SET DEFAULT 2;
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v4_frozen_plan(p_tenant uuid,p_run uuid,p_token uuid) RETURNS jsonb
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('version',b.plan_contract_version,'config',r.config_snapshot,'stages',coalesce((SELECT jsonb_agg(jsonb_build_object('phase',s.phase,'operation',coalesce(s.operation,''),'output_role',coalesce(s.output_role,''),'cardinality',s.cardinality) ORDER BY s.stage_index) FROM photo.apply_v4_stages s WHERE s.tenant_id=b.tenant_id AND s.run_id=b.run_id),'[]'::jsonb))
 FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND NOT r.cancel_requested AND r.status IN('queued','running','waiting_review')
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_v4_frozen_plan(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v4_frozen_plan(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_v4_frozen_plan(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.apply_effective_plan_style(p_config jsonb) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 result:=p_config->'style_config';
 IF jsonb_typeof(result->'extensions') IS DISTINCT FROM 'object' THEN result:=jsonb_set(result,'{extensions}','{}'::jsonb);END IF;
 -- Merge only the effective culling/series module slots relevant to this
 -- compatibility decision, using the same job→per-image→analysis precedence.
 IF p_config->'job_overrides'->'modules'->'extensions' ? 'culling' THEN result:=jsonb_set(result,'{extensions,culling}',p_config->'job_overrides'->'modules'->'extensions'->'culling');END IF;
 IF p_config->'job_overrides'->'modules'->'extensions' ? 'series' THEN result:=jsonb_set(result,'{extensions,series}',p_config->'job_overrides'->'modules'->'extensions'->'series');END IF;
 IF p_config->'override_config'->'modules'->'extensions' ? 'culling' THEN result:=jsonb_set(result,'{extensions,culling}',p_config->'override_config'->'modules'->'extensions'->'culling');END IF;
 IF p_config->'override_config'->'modules'->'extensions' ? 'series' THEN result:=jsonb_set(result,'{extensions,series}',p_config->'override_config'->'modules'->'extensions'->'series');END IF;
 IF jsonb_typeof(p_config->'graph_style_config')='object' THEN result:=p_config->'graph_style_config';END IF;
 -- Match readApplyCullStyle for already accepted legacy documents, including
 -- graph_style_config. The scanner must detect the same barrier as Go does.
 IF jsonb_typeof(result#>'{extensions,culling}') IS DISTINCT FROM 'object'
 AND jsonb_typeof(result->'culling')='object' THEN
  IF jsonb_typeof(result->'extensions') IS DISTINCT FROM 'object' THEN result:=jsonb_set(result,'{extensions}','{}'::jsonb);END IF;
  result:=jsonb_set(result,'{extensions,culling}',result->'culling');
 END IF;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_effective_plan_style(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_effective_plan_style(jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.request_apply_plan_restart(p_tenant uuid,p_run uuid,p_token uuid,p_diagnostic text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;style jsonb;culling boolean;series boolean;
BEGIN
 IF p_diagnostic NOT IN('APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING','APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT') OR p_diagnostic IS NULL THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR r.id IS NULL OR r.kind<>'apply' OR b.graph_version<>4 OR b.plan_contract_version<>1 OR (p_token IS NOT NULL AND (b.lease_token IS DISTINCT FROM p_token OR b.lease_until IS NULL OR b.lease_until<clock_timestamp())) OR r.cancel_requested OR r.status NOT IN('queued','running','waiting_review') THEN RETURN false;END IF;
 style:=photo.apply_effective_plan_style(r.config_snapshot);
 culling:=coalesce(style->'extensions'->'culling'->>'enabled','false')='true';series:=coalesce(style->'extensions'->'series'->>'enabled','false')='true';
 IF p_diagnostic='APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING' AND NOT(culling AND NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND phase='cull_review' AND cardinality='review')) THEN RETURN false;END IF;
 IF p_diagnostic='APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT' AND NOT(series AND b.item_count>128 AND NOT EXISTS(SELECT FROM photo.apply_v4_stages WHERE tenant_id=p_tenant AND run_id=p_run AND phase='series_pack' AND operation='series.pack')) THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.apply_builds SET phase='failed',lease_token=NULL,lease_until=NULL,last_error=p_diagnostic,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 -- The existing cancellation pipeline cancels/fences tasks, releases reserved
 -- quota exactly once, expires temporary artifacts, then settles cancelled.
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.audit_log(tenant_id,actor_type,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'system','apply.plan_restart_required','run',p_run,gen_random_uuid(),jsonb_build_object('diagnostic',p_diagnostic,'new_run_required',true,'automatic_retry',false));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.request_apply_plan_restart(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.request_apply_plan_restart(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.request_apply_plan_restart(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_unsupported_apply_plans() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;reason text;
BEGIN
 SELECT b.tenant_id,b.run_id,
 CASE WHEN coalesce(e.style->'extensions'->'culling'->>'enabled','false')='true' AND NOT EXISTS(SELECT FROM photo.apply_v4_stages s WHERE s.tenant_id=b.tenant_id AND s.run_id=b.run_id AND s.phase='cull_review' AND s.cardinality='review') THEN 'APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING' ELSE 'APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT' END AS diagnostic
 INTO candidate FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id CROSS JOIN LATERAL (SELECT photo.apply_effective_plan_style(r.config_snapshot) AS style) e
 WHERE b.graph_version=4 AND b.plan_contract_version=1 AND b.phase NOT IN('complete','failed') AND r.kind='apply' AND NOT r.cancel_requested AND r.status IN('queued','running','waiting_review')
 AND ((coalesce(e.style->'extensions'->'culling'->>'enabled','false')='true' AND NOT EXISTS(SELECT FROM photo.apply_v4_stages s WHERE s.tenant_id=b.tenant_id AND s.run_id=b.run_id AND s.phase='cull_review' AND s.cardinality='review')) OR (coalesce(e.style->'extensions'->'series'->>'enabled','false')='true' AND b.item_count>128 AND NOT EXISTS(SELECT FROM photo.apply_v4_stages s WHERE s.tenant_id=b.tenant_id AND s.run_id=b.run_id AND s.phase='series_pack' AND s.operation='series.pack')))
 ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN 0;END IF;
 IF photo.request_apply_plan_restart(candidate.tenant_id,candidate.run_id,NULL,candidate.diagnostic) THEN RETURN 1;END IF;
 RETURN 0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_unsupported_apply_plans() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_unsupported_apply_plans() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_unsupported_apply_plans() TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.apply_plan_restart_diagnostic(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT last_error FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run
 AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND last_error IN('APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING','APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT')
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_plan_restart_diagnostic(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_plan_restart_diagnostic(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_plan_restart_diagnostic(uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
