-- +goose Up
-- Current project editors can inspect adopted results without using the
-- producer author's session or requiring ownership of its private snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_originals_ready(p_tenant uuid,p_project uuid,p_task uuid,p_actor uuid,p_session uuid,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb;binding jsonb;source jsonb;canvas jsonb;canvas_jcs text;raw text;edge jsonb;role text;item photo.artifacts%ROWTYPE;n integer;v_ordinal integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_plan IS NULL OR pg_column_size(p_plan)>67108864 OR p_plan->>'params_jcs' IS NULL THEN RETURN false;END IF;
 params:=(p_plan->>'params_jcs')::jsonb;
 IF jsonb_typeof(params->'frames') IS DISTINCT FROM 'array' OR jsonb_typeof(p_plan->'sources_jcs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(params->'frames');
 IF n NOT BETWEEN 2 AND 30 OR jsonb_array_length(p_plan->'sources_jcs') IS DISTINCT FROM n
 OR NOT EXISTS(SELECT 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=(p_plan->>'snapshot_id')::uuid
 AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=(p_plan->>'actor_id')::uuid
 AND s.expires_at IS NULL AND s.item_count=n
 AND (SELECT count(*) FROM photo.snapshot_items si WHERE si.tenant_id=p_tenant AND si.snapshot_id=s.id)=n) THEN RETURN false;END IF;
 FOR v_ordinal IN 0..n-1 LOOP
 binding:=params->'frames'->v_ordinal;raw:=p_plan->'sources_jcs'->>v_ordinal;
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576
 OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM binding->'source'->>'sha256' THEN RETURN false;END IF;
 source:=raw::jsonb;
 canvas:=source->'canvas';
 IF jsonb_typeof(canvas) IS DISTINCT FROM 'object' OR NOT(canvas ?& ARRAY['height','width','orientation','source_sha256','space_id'])
 OR (canvas-'height'-'width'-'orientation'-'source_sha256'-'space_id')<>'{}'::jsonb
 OR jsonb_typeof(canvas->'height') IS DISTINCT FROM 'number' OR jsonb_typeof(canvas->'width') IS DISTINCT FROM 'number'
 OR coalesce(canvas->>'height','') !~ '^[0-9]+$' OR coalesce(canvas->>'width','') !~ '^[0-9]+$'
 OR (canvas->>'height')::bigint NOT BETWEEN 8 AND 200000000 OR (canvas->>'width')::bigint NOT BETWEEN 8 AND 200000000
 OR (canvas->>'width')::bigint>200000000/nullif((canvas->>'height')::bigint,0)
 OR canvas->'orientation' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(canvas->'source_sha256') IS DISTINCT FROM 'string' OR coalesce(canvas->>'source_sha256','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(canvas->'space_id') IS DISTINCT FROM 'string' OR coalesce(canvas->>'space_id','') !~ '^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$' THEN RETURN false;END IF;
 -- Narrow Canvas JCS: integer dimensions, orientation=1 and ASCII identifiers.
 -- The qualification-policy canonicalizer only supports numeric 1 and cannot serialize dimensions.
 canvas_jcs:='{"height":'||((canvas->>'height')::bigint)::text||',"orientation":1,"source_sha256":'||(canvas->'source_sha256')::text||',"space_id":'||(canvas->'space_id')::text||',"width":'||((canvas->>'width')::bigint)::text||'}';
 IF encode(sha256(convert_to(canvas_jcs,'UTF8')),'hex') IS DISTINCT FROM binding->>'canvas_hash'
 OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items si
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
 AND si.artifact_id::text=binding->'source'->>'artifact_id' AND si.sha256=binding->'source'->>'sha256') THEN RETURN false;END IF;
 IF source->'pixels' IS NULL OR source->'pixels'='null'::jsonb
 OR jsonb_typeof(source->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_array_length(source->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR role,edge IN SELECT 'WorkingPixels',source->'pixels' UNION ALL SELECT 'WorkingAlpha',source->'alpha'
 UNION ALL SELECT 'DenseWarpField',value->'inverse_field' FROM jsonb_array_elements(source->'transform_chain'->'steps') WHERE value->>'kind'='dense_warp' LOOP
 IF edge IS NULL OR edge='null'::jsonb THEN IF role<>'WorkingAlpha' THEN RETURN false;END IF;CONTINUE;END IF;
 SELECT * INTO item FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(edge->>'artifact_id')::uuid AND sha256=edge->>'sha256' FOR SHARE;
 IF NOT FOUND OR item.state IS DISTINCT FROM 'ready' OR item.verified_at IS NULL OR item.bytes IS NULL OR item.bytes<=0
 OR item.object_version IS NULL OR item.object_version IN('','null') OR item.expires_at<=clock_timestamp()
 OR (item.media_type IS DISTINCT FROM 'application/x-npy' AND NOT(role IN('WorkingPixels','WorkingAlpha') AND item.media_type='application/octet-stream'))
 OR (role='WorkingPixels' AND coalesce(item.output_role,'') NOT IN('working_rgb','pixels'))
 OR (role='WorkingAlpha' AND coalesce(item.output_role,'') NOT IN('working_alpha','alpha'))
 OR (role='DenseWarpField' AND item.output_role IS DISTINCT FROM 'warp_field')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=p_project
 AND i.contract='NpyArray' AND i.artifact_id=item.id AND i.sha256=item.sha256) THEN RETURN false;END IF;
 END LOOP;
 END LOOP;
 RETURN photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_adopted_multi_originals_ready(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_originals_ready(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- Exact adopted multi root only. No generic artifact access and no Apply grant.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_binding(p_tenant uuid,p_project uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_operation text)
RETURNS TABLE(result_id uuid,artifact_id uuid,sha256 text,run_id uuid,workflow_id uuid,snapshot_id uuid,params_jcs text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 z photo.task_results%ROWTYPE;a photo.artifacts%ROWTYPE;pa photo.artifacts%ROWTYPE;
 plan jsonb;params jsonb;v_run uuid;n integer;source jsonb;raw text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR coalesce(p_operation,'') NOT IN('optical.panorama','optical.focus_stack')
 OR p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT task.run_id INTO v_run FROM photo.tasks task JOIN photo.v2_workflows flow
 ON flow.tenant_id=task.tenant_id AND flow.run_id=task.run_id AND flow.project_id=p_project
 WHERE task.tenant_id=p_tenant AND task.id=p_task AND task.operation=p_operation;
 IF v_run IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND photo.tasks.run_id=v_run FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND photo.v2_workflows.run_id=v_run AND project_id=p_project FOR SHARE;
 IF r.id IS NULL OR t.id IS NULL OR w.id IS NULL OR t.operation IS DISTINCT FROM p_operation
 OR t.operation_version IS DISTINCT FROM '1' OR t.state IS DISTINCT FROM 'succeeded'
 OR t.cancel_requested IS DISTINCT FROM false OR r.cancel_requested IS DISTINCT FROM false
 OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true
 OR w.workflow_type IS DISTINCT FROM 'preview' OR w.renderer_version IS DISTINCT FROM 'v2.2'
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN;END IF;
 plan:=r.config_snapshot->'v29_multi_plan';raw:=plan->>'params_jcs';
 IF plan IS NULL OR raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 65536
 OR plan->>'tenant_id' IS DISTINCT FROM p_tenant::text OR plan->>'project_id' IS DISTINCT FROM p_project::text
 OR plan->>'run_id' IS DISTINCT FROM r.id::text OR plan->>'workflow_id' IS DISTINCT FROM w.id::text
 OR plan->>'task_id' IS DISTINCT FROM t.id::text OR plan->>'snapshot_id' IS DISTINCT FROM r.input_snapshot_id::text
 OR plan->>'actor_id' IS DISTINCT FROM r.created_by::text
 OR plan->>'operation' IS DISTINCT FROM p_operation
 OR plan->>'params_document_id' IS DISTINCT FROM t.params_artifact_id::text
 OR plan->>'params_sha' IS DISTINCT FROM encode(sha256(convert_to(raw,'UTF8')),'hex')
 OR r.v29_multi_queued_receipt->>'task_id' IS DISTINCT FROM t.id::text
 OR r.v29_multi_queued_receipt->>'run_id' IS DISTINCT FROM r.id::text
 OR r.v29_multi_queued_receipt->>'workflow_id' IS DISTINCT FROM w.id::text THEN RETURN;END IF;
 params:=raw::jsonb;
 IF params->'schema_version' IS DISTINCT FROM '1'::jsonb OR jsonb_typeof(params->'frames') IS DISTINCT FROM 'array' THEN RETURN;END IF;
 n:=jsonb_array_length(params->'frames');
 IF n NOT BETWEEN 2 AND CASE p_operation WHEN 'optical.panorama' THEN 20 ELSE 30 END
 OR jsonb_typeof(params->'reference_ordinal') IS DISTINCT FROM 'number'
 OR coalesce(params->>'reference_ordinal','') !~ '^[0-9]+$'
 OR (params->>'reference_ordinal')::integer NOT BETWEEN 0 AND n-1
 OR (SELECT count(DISTINCT value->>'asset_version_id') FROM jsonb_array_elements(params->'frames'))<>n THEN RETURN;END IF;
 IF photo.v29_adopted_multi_originals_ready(p_tenant,p_project,p_task,p_actor,p_session,plan) IS DISTINCT FROM true THEN RETURN;END IF;
 FOR source IN SELECT value FROM jsonb_array_elements(params->'frames') LOOP
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
 AND i.project_id=p_project AND i.contract='WorkingImage'
 AND i.artifact_id::text=source->'source'->>'artifact_id' AND i.sha256=source->'source'->>'sha256') THEN RETURN;END IF;
 END LOOP;
 SELECT * INTO pa FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id FOR SHARE;
 IF pa.id IS NULL OR pa.sha256 IS DISTINCT FROM plan->>'params_sha' OR pa.state IS DISTINCT FROM 'ready'
 OR pa.verified_at IS NULL OR pa.media_type IS DISTINCT FROM 'application/json' OR pa.bytes IS DISTINCT FROM octet_length(raw)::bigint
 OR pa.object_version IS NULL OR pa.object_version IN('','null') OR pa.expires_at<=clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
 AND i.contract='Params' AND i.artifact_id=pa.id AND i.sha256=pa.sha256) THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id
 AND task_id=t.id AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 IF z.id IS NULL OR z.review_required IS DISTINCT FROM true OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' THEN RETURN;END IF;
 IF jsonb_array_length(z.outputs)<>1 THEN RETURN;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(z.outputs->0->>'artifact_id')::uuid FOR SHARE;
 IF a.id IS NULL OR a.sha256 IS DISTINCT FROM z.outputs->0->>'sha256'
 OR a.output_role IS DISTINCT FROM 'document' OR a.media_type IS DISTINCT FROM 'application/json'
 OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR a.bytes IS NULL OR a.bytes NOT BETWEEN 2 AND 1048576 OR a.expires_at<=clock_timestamp()
 OR a.created_by_task_id IS DISTINCT FROM t.id OR a.produced_attempt IS DISTINCT FROM t.attempt OR a.produced_fence IS DISTINCT FROM t.fencing_token
 OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id
 AND l.project_id=p_project AND l.artifact_id=a.id AND l.sha256=a.sha256)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i JOIN photo.run_events e ON e.tenant_id=i.tenant_id
 AND e.run_id=r.id AND e.type='v2.task.adopted' AND e.payload->>'task_id'=t.id::text AND e.payload->>'result_id'=z.id::text
 WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT z.id,a.id,a.sha256,r.id,w.id,r.input_snapshot_id,raw;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_multi_binding(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_multi_object(p_tenant uuid,p_project uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_operation text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v29_adopted_multi_binding(p_tenant,p_project,p_task,p_actor,p_session,p_operation) b
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=b.artifact_id AND a.sha256=b.sha256
 WHERE b.artifact_id=p_artifact AND b.sha256=p_sha AND coalesce(p_sha,'') ~ '^[0-9a-f]{64}$';
$$;
ALTER FUNCTION photo.v29_adopted_multi_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_multi_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_multi_object(uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
