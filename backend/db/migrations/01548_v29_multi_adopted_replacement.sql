-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_adopted_replacement_frame_ready(p_tenant uuid,p_project uuid,p_task uuid,p_plan jsonb,p_ordinal integer,p_binding jsonb,p_raw text,p_source jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_plan jsonb;located jsonb;run_id uuid;
BEGIN
 SELECT t.run_id INTO run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task;
 current_plan:=photo.v29_multi_execution_plan(p_tenant,run_id);
 IF current_plan IS NULL OR current_plan IS DISTINCT FROM p_plan
 OR NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=run_id AND r.config_snapshot ? 'v29_negative_replacement_draft_jcs')
 OR ((p_plan->>'params_jcs')::jsonb)->'frames'->p_ordinal IS DISTINCT FROM p_binding
 OR p_plan->'sources_jcs'->>p_ordinal IS DISTINCT FROM p_raw
 OR p_raw::jsonb IS DISTINCT FROM p_source THEN RETURN false;END IF;
 located:=photo.v29_multi_replacement_source(p_tenant,p_project,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,p_task,p_binding);
 IF located IS NULL OR located->>'bytes' IS DISTINCT FROM octet_length(p_raw)::text THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items si JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
 AND v.master_artifact_id=si.artifact_id AND v.content_hash=si.sha256 AND v.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.project_id=p_project
 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 AND original.sha256=p_source->'canvas'->>'source_sha256' AND original.state='ready' AND original.verified_at IS NOT NULL
 AND original.bytes>0 AND original.object_version NOT IN('','null') AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 WHERE si.tenant_id=p_tenant AND si.snapshot_id=(p_plan->>'snapshot_id')::uuid AND si.ordinal=p_ordinal
 AND si.asset_version_id::text=p_binding->>'asset_version_id');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_multi_adopted_replacement_frame_ready(uuid,uuid,uuid,jsonb,integer,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_adopted_replacement_frame_ready(uuid,uuid,uuid,jsonb,integer,jsonb,text,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text;old_text text;
BEGIN
 definition:=pg_get_functiondef('photo.v29_adopted_multi_originals_ready(uuid,uuid,uuid,uuid,uuid,jsonb)'::regprocedure);
 old_text:=$baseline$NOT EXISTS(SELECT 1 FROM photo.snapshot_items si
 JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
 AND v.master_artifact_id=si.artifact_id AND v.content_hash=si.sha256 AND v.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts a ON a.tenant_id=si.tenant_id AND a.id=si.artifact_id AND a.sha256=si.sha256
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='working_image'
 AND a.media_type='application/json' AND a.bytes=octet_length(raw)
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 AND original.sha256=source->'canvas'->>'source_sha256' AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null') AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 WHERE si.tenant_id=p_tenant AND si.snapshot_id=(p_plan->>'snapshot_id')::uuid AND si.ordinal=v_ordinal
 AND si.asset_version_id::text=binding->>'asset_version_id'
 AND si.artifact_id::text=binding->'source'->>'artifact_id' AND si.sha256=binding->'source'->>'sha256')$baseline$;
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'MULTI_ADOPTED_FRAME_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_text,'('||old_text||' AND photo.v29_multi_adopted_replacement_frame_ready(p_tenant,p_project,p_task,p_plan,v_ordinal,binding,raw,source) IS DISTINCT FROM true)');
 definition:=pg_get_functiondef('photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text)'::regprocedure);
 old_text:='plan:=r.config_snapshot->''v29_multi_plan'';';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'MULTI_ADOPTED_PLAN_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_text,'plan:=photo.v29_multi_execution_plan(p_tenant,r.id);');
 old_text:='r.v29_multi_queued_receipt';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'MULTI_ADOPTED_RECEIPT_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_text,'(CASE WHEN r.config_snapshot ? ''v29_negative_replacement_draft_jcs'' THEN r.v29_negative_replacement_queued_receipt ELSE r.v29_multi_queued_receipt END)');
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.v29_multi_preview_source(uuid,uuid,text)'::regprocedure);
 old_text:='r.config_snapshot->''v29_multi_plan'' AS body';
 IF strpos(definition,old_text)=0 THEN RAISE EXCEPTION 'MULTI_PREVIEW_PLAN_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_text,'photo.v29_multi_execution_plan(p_tenant,r.id) AS body');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
