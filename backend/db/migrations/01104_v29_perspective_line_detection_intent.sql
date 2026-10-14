-- +goose Up
-- Source-authorized draft only. No task, reservation or queue activation.
ALTER TABLE photo.runs ADD COLUMN v29_perspective_line_detection boolean NOT NULL DEFAULT false;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_line_detection_kind CHECK(
 NOT v29_perspective_line_detection OR (kind='v2' AND failure_policy='fail_fast' AND v26_derivative_task_id IS NULL
 AND v29_perspective_preview_task_id IS NULL AND v29_perspective_quality_task_id IS NULL));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_perspective_line_detection_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_perspective_line_detection AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Line detection requires trusted admission' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_perspective_line_detection IS DISTINCT FROM OLD.v29_perspective_line_detection
 OR (OLD.v29_perspective_line_detection AND
 ROW(NEW.id,NEW.tenant_id,NEW.kind,NEW.created_by,NEW.input_snapshot_id,NEW.config_snapshot,NEW.config_hash,NEW.failure_policy)
 IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.kind,OLD.created_by,OLD.input_snapshot_id,OLD.config_snapshot,OLD.config_hash,OLD.failure_policy)) THEN
  RAISE EXCEPTION 'Line detection intent is immutable' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_perspective_line_detection_intent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_perspective_line_detection_intent() FROM PUBLIC;
CREATE TRIGGER runs_v29_line_detection_intent_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_perspective_line_detection_intent();

CREATE FUNCTION photo.save_v29_perspective_line_detection_intent(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb; source jsonb; params jsonb; profile jsonb; seed jsonb; config jsonb;
 v_run uuid; v_project uuid; v_snapshot uuid; v_task uuid; source_id uuid; version_id uuid;
 pixels bigint; per_pixel bigint; h bytea; identity uuid; old photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 2097152
 THEN RETURN NULL;END IF;
 plan:=p_plan::jsonb;
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM ARRAY[
 'actor_id','auth_session_id','base','params_jcs','profile_jcs','project_id','run_id','schema_version',
 'seed_jcs','snapshot_id','source_jcs','task_id','tenant_id','working_icc_sha']::text[]
 OR plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR plan->>'actor_id' IS DISTINCT FROM p_actor::text
 OR plan->>'auth_session_id' IS DISTINCT FROM p_session::text OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR coalesce(plan->>'working_icc_sha','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(plan->'source_jcs') IS DISTINCT FROM 'string' OR octet_length(plan->>'source_jcs') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(plan->'params_jcs') IS DISTINCT FROM 'string' OR octet_length(plan->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(plan->'profile_jcs') IS DISTINCT FROM 'string' OR octet_length(plan->>'profile_jcs') NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(plan->'seed_jcs') IS DISTINCT FROM 'string' OR octet_length(plan->>'seed_jcs') NOT BETWEEN 2 AND 65536
 THEN RETURN NULL;END IF;
 v_run:=(plan->>'run_id')::uuid;v_project:=(plan->>'project_id')::uuid;
 v_snapshot:=(plan->>'snapshot_id')::uuid;v_task:=(plan->>'task_id')::uuid;
 source_id:=(plan->'base'->'source'->>'artifact_id')::uuid;version_id:=(plan->'base'->>'asset_version_id')::uuid;
 IF v_run IS NULL OR v_project IS NULL OR v_snapshot IS NULL OR v_task IS NULL OR source_id IS NULL OR version_id IS NULL
 OR '00000000-0000-0000-0000-000000000000'::uuid=ANY(ARRAY[v_run,v_project,v_snapshot,v_task,source_id,version_id]) THEN RETURN NULL;END IF;
 source:=(plan->>'source_jcs')::jsonb;params:=(plan->>'params_jcs')::jsonb;
 profile:=(plan->>'profile_jcs')::jsonb;seed:=(plan->>'seed_jcs')::jsonb;
 IF params IS DISTINCT FROM jsonb_build_object('schema_version',1,'base',plan->'base')
 OR seed IS DISTINCT FROM jsonb_build_object('tenant',p_tenant,'project',v_project,'params',params,'working_icc_sha',plan->>'working_icc_sha')
 OR encode(sha256(convert_to(plan->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM plan->'base'->'source'->>'sha256'
 OR source->>'dtype' IS DISTINCT FROM 'float32'
 OR source->'color'->>'icc_sha256' IS DISTINCT FROM plan->>'working_icc_sha'
 OR (source->'canvas'->>'width')::bigint NOT BETWEEN 2 AND 200000000
 OR (source->'canvas'->>'height')::bigint NOT BETWEEN 2 AND 200000000 THEN RETURN NULL;END IF;
 pixels:=(source->'canvas'->>'width')::bigint*(source->'canvas'->>'height')::bigint;
 IF pixels IS NULL OR pixels>200000000 THEN RETURN NULL;END IF;
 per_pixel:=CASE WHEN source->'alpha'='null'::jsonb THEN 12 ELSE 16 END;
 IF profile IS DISTINCT FROM jsonb_build_object('backend','cpu','precision','float32','renderer_version','v2.2',
 'resource_estimate',jsonb_build_object('pixel_count',pixels,'ram_bytes',pixels*per_pixel+268435456,
 'scratch_bytes',pixels*per_pixel+33554432,'output_bytes',1048576,'vram_bytes',0)) THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(v_run)||convert_to('V29.PerspectiveLines:'||(plan->>'seed_jcs'),'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 identity:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF identity IS DISTINCT FROM v_task THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=v_project AND archived_at IS NULL FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=version_id FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source_id AND sha256=plan->'base'->'source'->>'sha256'
 AND bytes=octet_length(plan->>'source_jcs') AND state='ready' AND verified_at IS NOT NULL
 AND object_version IS NOT NULL AND object_version NOT IN('','null') FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 IF photo.v28_local_portrait_source_scope(p_tenant,v_project,v_snapshot,p_actor,p_session,plan->'base') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 config:=jsonb_build_object('project_id',v_project,'v29_perspective_line_detection',plan);
 SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;
 IF FOUND THEN
  IF old.tenant_id=p_tenant AND old.v29_perspective_line_detection AND old.created_by=p_actor
   AND old.input_snapshot_id=v_snapshot AND old.config_snapshot=config AND old.status='draft' AND old.failure_policy='fail_fast'
   AND NOT old.graph_sealed AND NOT old.cancel_requested THEN RETURN v_run;END IF;
  RETURN NULL;
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v29_perspective_line_detection,failure_policy)
 VALUES(v_run,p_tenant,'v2','draft',v_snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,false,true,'fail_fast');
 RETURN v_run;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.save_v29_perspective_line_detection_intent(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_perspective_line_detection_intent(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v29_perspective_line_detection_intent(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
