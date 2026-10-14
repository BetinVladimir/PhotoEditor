-- +goose Up
-- Metadata gate only: original WorkingImage/array bytes remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_replacement_source(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_base jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_id uuid;plan jsonb;packet jsonb;identity jsonb;base jsonb;original_plan jsonb;original_params text;original_frame jsonb;intent jsonb;source photo.artifacts%ROWTYPE;
 mapped jsonb;entry jsonb;selected jsonb;parent jsonb;origin uuid;origin_run uuid;namespace uuid;parent_task uuid;parent_run uuid;valid boolean:=false;
BEGIN
 SELECT t.run_id INTO run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation IN('optical.panorama','optical.focus_stack') AND t.operation_version='1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 plan:=photo.v29_multi_execution_plan(p_tenant,run_id);
 SELECT (r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb INTO packet FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=run_id;
 identity:=packet->'identity';base:=p_base;
 IF plan IS NULL OR identity->>'operation' IS NULL OR identity->>'operation' NOT IN('optical.panorama','optical.focus_stack')
 OR plan->>'actor_id' IS DISTINCT FROM p_actor::text OR plan->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR plan->>'project_id' IS DISTINCT FROM p_project::text OR plan->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements((plan->>'params_jcs')::jsonb->'frames') frame WHERE frame=base) THEN RETURN NULL;END IF;
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,identity->>'decision_hash',(identity->>'original_task_id')::uuid);
 SELECT coalesce(r.config_snapshot->'v29_multi_plan',((r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb)->'draft') INTO original_plan
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=(identity->>'original_run_id')::uuid;
 original_params:=original_plan->>'params_jcs';
 IF intent IS NULL OR original_params IS NULL OR encode(sha256(convert_to(original_params,'UTF8')),'hex') IS DISTINCT FROM intent->'params'->>'sha256' THEN RETURN NULL;END IF;
 SELECT frame INTO original_frame FROM jsonb_array_elements(original_params::jsonb->'frames') frame WHERE frame->>'asset_version_id'=base->>'asset_version_id';
 IF original_frame IS NULL THEN RETURN NULL;END IF;
 -- Asset identity remains tied to the immutable original consumer snapshot.
 IF NOT EXISTS(SELECT 1 FROM photo.snapshot_items si JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE si.tenant_id=p_tenant AND si.snapshot_id=(plan->>'snapshot_id')::uuid AND si.asset_version_id=(base->>'asset_version_id')::uuid
 AND v.review_state='accepted' AND v.master_artifact_id=si.artifact_id AND v.content_hash=si.sha256
 AND a.project_id=p_project AND a.deleted_at IS NULL) THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(base->'source'->>'artifact_id')::uuid
 AND a.sha256=base->'source'->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 1048576 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project
 AND i.task_id=p_task AND i.contract='WorkingImage' AND i.artifact_id=source.id AND i.sha256=source.sha256) THEN RETURN NULL;END IF;
 mapped:=photo.read_v29_negative_affected_source_inputs(p_tenant,p_project,p_actor,p_session,
 (identity->>'review_id')::uuid,identity->>'decision_hash',(identity->>'original_task_id')::uuid);
 IF mapped IS NULL THEN RETURN NULL;END IF;
 -- Match the affected edge for this exact original frame, not any producer.
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(mapped) e WHERE e->>'artifact_id'=original_frame->'source'->>'artifact_id' AND e->>'sha256'=original_frame->'source'->>'sha256') THEN
 valid:=base=original_frame AND photo.read_v29_negative_unchanged_source(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,identity->>'decision_hash',(identity->>'original_task_id')::uuid,source.id,source.sha256) IS NOT NULL;
 ELSE
 SELECT i.origin_task_id,t.run_id INTO origin,origin_run FROM photo.v29_negative_invalidations i
 JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.origin_task_id
 WHERE i.tenant_id=p_tenant AND i.task_id=(identity->>'original_task_id')::uuid
 AND i.review_id=(identity->>'review_id')::uuid AND i.decision_hash=identity->>'decision_hash' AND i.route='base';
 IF NOT FOUND THEN RETURN NULL;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||(identity->>'review_id')||':'||(identity->>'decision_hash'));
 FOR entry IN SELECT value FROM jsonb_array_elements(mapped) WHERE value->>'artifact_id'=original_frame->'source'->>'artifact_id' AND value->>'sha256'=original_frame->'source'->>'sha256' LOOP
 IF entry->>'producer_task_id'=origin::text THEN
 selected:=photo.read_v29_negative_selected_source(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,origin,origin_run,identity->>'decision_hash','base');
 valid:=selected->'source'->>'artifact_id'=source.id::text AND selected->'source'->>'sha256'=source.sha256;
 ELSE
 parent_task:=photo.v30_uuid5(namespace,'task:'||(entry->>'producer_task_id'));
 parent_run:=photo.v30_uuid5(namespace,'run:'||(entry->>'producer_run_id')||':'||(entry->>'producer_task_id'));
 IF photo.v29_negative_replacement_member(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,
 identity->>'decision_hash',(entry->>'producer_task_id')::uuid,parent_task,parent_run) THEN
 parent:=photo.read_v29_adopted_optical_parent(p_tenant,p_project,p_actor,p_session,parent_task,parent_run);
 valid:=parent->'source'->>'artifact_id'=source.id::text AND parent->'source'->>'sha256'=source.sha256;
 END IF;
 END IF;
 IF valid IS TRUE THEN EXIT;END IF;
 END LOOP;
 END IF;
 IF valid IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',source.id,'sha256',source.sha256,'bucket',source.bucket,'object_key',source.object_key,
 'object_version',source.object_version,'bytes',source.bytes,'media_type',source.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_replacement_source(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_replacement_source(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_execution_source_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_task uuid,p_base jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;source jsonb;previous_tenant text;answer boolean;
BEGIN
 previous_tenant:=current_setting('app.tenant_id',true);
 IF previous_tenant IS NOT NULL AND previous_tenant<>'' AND previous_tenant<>p_tenant::text THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT run.* INTO r FROM photo.tasks task JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
 WHERE task.tenant_id=p_tenant AND task.id=p_task AND task.operation IN('optical.panorama','optical.focus_stack') AND task.operation_version='1';
 IF r.id IS NULL OR r.input_snapshot_id IS DISTINCT FROM p_snapshot OR r.created_by IS DISTINCT FROM p_actor THEN
 answer:=false;
 ELSIF r.config_snapshot ? 'v29_negative_replacement_draft_jcs' THEN
 plan:=photo.v29_multi_execution_plan(p_tenant,r.id);
 source:=photo.v29_multi_replacement_source(p_tenant,p_project,p_actor,p_session,p_task,p_base);
 answer:=plan IS NOT NULL AND source IS NOT NULL
 AND plan->>'task_id'=p_task::text AND plan->>'project_id'=p_project::text
 AND plan->>'auth_session_id'=p_session::text
 AND EXISTS(SELECT 1 FROM jsonb_array_elements((plan->>'params_jcs')::jsonb->'frames') frame WHERE frame=p_base)
 AND source->>'artifact_id'=p_base->'source'->>'artifact_id'
 AND source->>'sha256'=p_base->'source'->>'sha256';
 ELSE
 answer:=photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base);
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 RETURN coalesce(answer,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN false;
END $$;
ALTER FUNCTION photo.v29_multi_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text;old_scope text;
BEGIN
 definition:=pg_get_functiondef('photo.v29_worker_multi_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'::regprocedure);
 old_scope:='photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,binding)';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'MULTI_WORKER_SOURCE_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_scope,'photo.v29_multi_execution_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,auth_session,p_task,binding)');
 definition:=pg_get_functiondef('photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)'::regprocedure);
 old_scope:='photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'MULTI_ADOPTION_SOURCE_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_scope,'photo.v29_multi_execution_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,');
 old_scope:='photo.v29_multi_execution_session(p_tenant,r.id,r.config_snapshot),binding)';
 IF strpos(definition,old_scope)=0 THEN RAISE EXCEPTION 'MULTI_ADOPTION_TASK_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_scope,'photo.v29_multi_execution_session(p_tenant,r.id,r.config_snapshot),p_task,binding)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
