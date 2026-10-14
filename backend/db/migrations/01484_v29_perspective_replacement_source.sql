-- +goose Up
-- Metadata gate only: original WorkingImage/array bytes remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_replacement_source(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_id uuid;plan jsonb;packet jsonb;identity jsonb;base jsonb;source photo.artifacts%ROWTYPE;
 mapped jsonb;entry jsonb;selected jsonb;parent jsonb;origin uuid;origin_run uuid;namespace uuid;parent_task uuid;parent_run uuid;valid boolean:=false;
BEGIN
 SELECT t.run_id INTO run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.perspective' AND t.operation_version='1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 plan:=photo.v29_perspective_execution_plan(p_tenant,run_id);
 SELECT (r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb INTO packet FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=run_id;
 identity:=packet->'identity';base:=(plan->>'params_jcs')::jsonb->'base';
 IF plan IS NULL OR identity->>'operation' IS DISTINCT FROM 'optical.perspective'
 OR plan->>'actor_id' IS DISTINCT FROM p_actor::text OR plan->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR plan->>'project_id' IS DISTINCT FROM p_project::text OR plan->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN NULL;END IF;
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
 -- Unchanged source must be an exact original registered WorkingImage.
 IF jsonb_array_length(mapped)=0 THEN
 valid:=EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project
 AND i.task_id=(identity->>'original_task_id')::uuid AND i.contract='WorkingImage' AND i.artifact_id=source.id AND i.sha256=source.sha256);
 ELSE
 SELECT i.origin_task_id,t.run_id INTO origin,origin_run FROM photo.v29_negative_invalidations i
 JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.origin_task_id
 WHERE i.tenant_id=p_tenant AND i.task_id=(identity->>'original_task_id')::uuid
 AND i.review_id=(identity->>'review_id')::uuid AND i.decision_hash=identity->>'decision_hash' AND i.route='base';
 IF NOT FOUND THEN RETURN NULL;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||(identity->>'review_id')||':'||(identity->>'decision_hash'));
 FOR entry IN SELECT value FROM jsonb_array_elements(mapped) LOOP
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
ALTER FUNCTION photo.v29_perspective_replacement_source(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_replacement_source(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
