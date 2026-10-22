-- +goose Up
-- Receipt authority is a live final-run binding, never artifact possession.
-- These readers augment the ordinary render admission/replay readers.
ALTER TABLE photo.editor_effect_builds
 ADD COLUMN effect_source_artifact_id uuid,
 ADD COLUMN effect_source_sha text CHECK(effect_source_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN effect_source_canvas_hash text CHECK(effect_source_canvas_hash ~ '^[0-9a-f]{64}$'),
 ADD COLUMN effect_source_result_artifact_id uuid,
 ADD COLUMN effect_source_result_sha text CHECK(effect_source_result_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN precompose_task_id uuid,
 ADD COLUMN precompose_result_id uuid,
 ADD COLUMN precompose_attempt integer,
 ADD COLUMN precompose_fence bigint,
 ADD CONSTRAINT editor_effect_source_ref_pair CHECK((effect_source_artifact_id IS NULL)=(effect_source_sha IS NULL)),
 ADD CONSTRAINT editor_effect_source_result_pair CHECK((effect_source_result_artifact_id IS NULL)=(effect_source_result_sha IS NULL));
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_render_receipt(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.editor_effect_builds b
 JOIN photo.tasks t ON t.tenant_id=b.tenant_id AND t.run_id=b.final_run_id AND t.id=p_task
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tasks seal ON seal.tenant_id=b.tenant_id AND seal.id=b.seal_task_id AND seal.run_id=b.seal_run_id
 JOIN photo.task_results result ON result.tenant_id=seal.tenant_id AND result.id=seal.canonical_result_id AND result.task_id=seal.id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.seal_receipt_artifact_id AND a.sha256=b.seal_receipt_sha
  AND a.created_by_task_id=seal.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
 WHERE b.tenant_id=p_tenant AND b.final_run_id=p_run AND b.state IN('composing','complete')
  AND NOT r.cancel_requested AND NOT t.cancel_requested AND NOT seal.cancel_requested AND seal.state='succeeded'
  AND a.output_role='executed_base_receipt' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  AND EXISTS(SELECT FROM photo.artifacts root WHERE root.tenant_id=b.tenant_id AND root.id=b.result_artifact_id AND root.sha256=b.result_sha
   AND root.created_by_task_id=seal.id AND root.produced_attempt=result.attempt AND root.produced_fence=result.fencing_token
   AND root.output_role='image_result' AND root.state='ready' AND root.verified_at IS NOT NULL
   AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null') AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
   AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256)))

  AND photo.v2_editor_authorized(p_tenant,b.project_id,b.actor_id,b.session_id)
$$;
ALTER FUNCTION photo.editor_effect_render_receipt(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_render_receipt(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
CREATE FUNCTION photo.editor_effect_render_worker_receipt(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT receipt.* FROM photo.editor_effect_render_receipt(p_tenant,p_run,p_task) receipt
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=p_task AND t.run_id=p_run
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.state='running' AND t.operation='render.compose' AND t.attempt=p_attempt AND t.fencing_token=p_fence
  AND t.worker_id=p_worker AND t.params_artifact_id=p_params AND t.input_digest=p_digest
  AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND r.status='running' AND r.graph_sealed
  AND octet_length(p_json) BETWEEN 2 AND 1048576 AND encode(sha256(convert_to(p_json,'UTF8')),'hex')=p_sha
  AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_params AND a.sha256=p_sha AND a.state='ready')
  AND EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
  AND p_json::jsonb#>'{recipe,executed_base_receipt}'=jsonb_build_object('artifact_id',receipt.artifact_id,'sha256',receipt.sha256)
  AND EXISTS(SELECT FROM photo.editor_effect_builds b WHERE b.tenant_id=p_tenant AND b.final_run_id=p_run
   AND p_json::jsonb#>'{recipe,source}'=jsonb_build_object('asset_version_id',b.asset_version_id,'source',jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha),'canvas_hash',b.processed_source_canvas_hash)
   AND p_json::jsonb#>'{recipe,base_style}'=jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha)
   AND p_json::jsonb#>'{recipe,overrides}'='[]'::jsonb
   AND p_json::jsonb#>'{recipe,session_corrections}'='null'::jsonb
   AND p_json::jsonb#>'{recipe,learning_version_id}'='null'::jsonb)

$$;
ALTER FUNCTION photo.editor_effect_render_worker_receipt(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_render_worker_receipt(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_render_worker_receipt(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
CREATE FUNCTION photo.editor_effect_render_adoption_receipt(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT receipt.* FROM photo.tasks t
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=p_result AND result.task_id=t.id
 CROSS JOIN LATERAL photo.editor_effect_render_receipt(p_tenant,t.run_id,p_task) receipt
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='render.compose'
  AND t.state='succeeded' AND t.canonical_result_id=p_result AND t.attempt=p_attempt AND t.fencing_token=p_fence
  AND result.attempt=p_attempt AND result.fencing_token=p_fence
$$;
ALTER FUNCTION photo.editor_effect_render_adoption_receipt(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_render_adoption_receipt(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_render_adoption_receipt(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- Precomposition is restricted to the first stage of a dedicated origin.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
 VALUES('editor.layers_precompose','1','cpu','cpu.editor_layers_precompose',NULL,300,'["cpu"]'::jsonb);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('editor.layers_precompose','1','pixels','["application/x-npy"]'::jsonb,2400008192,1),
 ('editor.layers_precompose','1','alpha','["application/x-npy"]'::jsonb,800008192,1),
 ('editor.layers_precompose','1','mask_pixels','["application/x-npy"]'::jsonb,800008192,2),
 ('editor.layers_precompose','1','mask','["application/json"]'::jsonb,2097152,2),
 ('editor.layers_precompose','1','working_image','["application/json"]'::jsonb,2097152,1),
 ('editor.layers_precompose','1','image_report','["application/json"]'::jsonb,2097152,1),
 ('editor.layers_precompose','1','image_result','["application/json"]'::jsonb,2097152,1);
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure);
 needle:='allowed constant text[]:=ARRAY[';IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_PLAN_ALLOWLIST_ANCHOR';END IF;
 definition:=replace(definition,needle,needle||'''editor.layers_precompose'',');
 needle:='phase IN(''mask'',''resolve'')';IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_PLAN_BOUNDARY_ANCHOR';END IF;
 definition:=replace(definition,needle,'(phase IN(''mask'',''resolve'') OR phase=''editor_precompose'' AND EXISTS(SELECT FROM photo.editor_effect_builds e WHERE e.tenant_id=p_tenant AND e.run_id=p_run))');
 needle:='SELECT * INTO b FROM photo.apply_builds';IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_PLAN_ORIGIN_ANCHOR';END IF;
 definition:=replace(definition,needle,'IF EXISTS(SELECT FROM jsonb_array_elements(p_plan) x WHERE x->>''operation''=''editor.layers_precompose'') AND (p_plan->0->>''phase''<>''editor_precompose'' OR p_plan->0->>''operation''<>''editor.layers_precompose'' OR NOT EXISTS(SELECT FROM photo.editor_effect_builds e WHERE e.tenant_id=p_tenant AND e.run_id=p_run)) THEN RETURN false;END IF; '||needle);
 EXECUTE definition;
 definition:=pg_get_functiondef('photo.register_apply_v4_node(uuid,uuid,uuid,jsonb)'::regprocedure);
 needle:='jsonb_array_length(p_node->''inputs'') NOT BETWEEN 1 AND 128';IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_NODE_INPUT_BOUND_ANCHOR';END IF;
 definition:=replace(definition,needle,'jsonb_array_length(p_node->''inputs'') NOT BETWEEN 1 AND (CASE WHEN p_node->>''operation''=''editor.layers_precompose'' THEN 4096 ELSE 128 END)');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.editor_layers_precompose_worker_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;o photo.editor_effect_builds%ROWTYPE;n photo.apply_nodes%ROWTYPE;slot photo.resource_slots%ROWTYPE;body jsonb;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 1048576 OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO o FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR SHARE;
 SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF o.run_id IS NULL OR o.state<>'building' OR r.id IS NULL OR r.cancel_requested OR r.status NOT IN('running','queued')
  OR t.id IS NULL OR t.state<>'running' OR t.cancel_requested OR t.operation<>'editor.layers_precompose' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
  OR t.worker_id IS DISTINCT FROM p_worker OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
  OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp() OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
  OR n.id IS NULL OR n.phase<>'editor_precompose' OR n.params_sha256 IS DISTINCT FROM p_sha OR n.params IS DISTINCT FROM p_json::jsonb
  OR t.inputs IS DISTINCT FROM r.config_snapshot#>'{editor_precompose,inputs}'
  OR slot.id IS NULL OR slot.resource_class<>'cpu' OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.lease_until<=clock_timestamp()
  OR t.execution_profile->>'backend'<>'cpu'
  OR photo.worker_authorized(p_tenant,p_worker,'cpu',slot.node_id,t.operation,'cpu',NULL) IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=p_json::jsonb;
 IF body->'schema_version' IS DISTINCT FROM '1'::jsonb OR body->>'build_run_id' IS DISTINCT FROM p_run::text
  OR body->'original_recipe' IS DISTINCT FROM jsonb_build_object('artifact_id',o.frozen_recipe_artifact_id,'sha256',o.frozen_recipe_sha)
  OR coalesce(body->'source_result','null'::jsonb) IS DISTINCT FROM coalesce(r.config_snapshot#>'{editor_precompose,source_result}','null'::jsonb)
  OR body->'source' IS DISTINCT FROM jsonb_build_object('asset_version_id',o.asset_version_id,'source',jsonb_build_object('artifact_id',o.source_artifact_id,'sha256',o.source_sha),'canvas_hash',o.source_canvas_hash)
  OR NOT photo.editor_effect_origin_scope(p_tenant,o.project_id,o.asset_version_id,o.source_artifact_id,o.source_sha,o.frozen_recipe_artifact_id,o.frozen_recipe_sha,o.frozen_document_id,o.draft_document_id,o.draft_revision,o.actor_id,o.session_id)
  OR NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
  OR NOT EXISTS(SELECT FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='task_params' AND d.artifact_id=p_params AND d.sha256=p_sha AND d.bytes=octet_length(p_json) AND d.state='ready')
  OR EXISTS(SELECT FROM jsonb_array_elements(t.inputs) i LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(i->>'artifact_id')::uuid AND a.sha256=i->>'sha256'
   WHERE a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.record_editor_precompose(p_tenant uuid,p_run uuid,p_token uuid,p_artifact uuid,p_sha text,p_canvas text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;o photo.editor_effect_builds%ROWTYPE;t photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;root photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 SELECT * INTO o FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF o.run_id IS NULL OR o.state<>'building' OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR p_canvas IS DISTINCT FROM o.source_canvas_hash THEN RETURN false;END IF;
 SELECT task.* INTO t FROM photo.tasks task JOIN photo.apply_nodes n ON n.tenant_id=task.tenant_id AND n.id=task.id AND n.run_id=task.run_id
 WHERE task.tenant_id=p_tenant AND task.run_id=p_run AND n.phase='editor_precompose' AND task.operation='editor.layers_precompose' AND task.state='succeeded' AND NOT task.cancel_requested AND task.canonical_result_id IS NOT NULL;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.created_by_task_id=t.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.output_role='working_image'
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))) THEN RETURN false;END IF;
 SELECT a.* INTO root FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
  AND a.output_role='image_result' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF root.id IS NULL THEN RETURN false;END IF;
 IF o.effect_source_artifact_id IS NOT NULL THEN RETURN o.effect_source_artifact_id=p_artifact AND o.effect_source_sha=p_sha AND o.precompose_result_id=result.id;END IF;
 UPDATE photo.editor_effect_builds SET effect_source_artifact_id=p_artifact,effect_source_sha=p_sha,effect_source_canvas_hash=p_canvas,
  effect_source_result_artifact_id=root.id,effect_source_result_sha=root.sha256,precompose_task_id=t.id,precompose_result_id=result.id,precompose_attempt=result.attempt,precompose_fence=result.fencing_token,updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND run_id=p_run;
 RETURN true;
END $$;
ALTER FUNCTION photo.record_editor_precompose(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_editor_precompose(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_editor_precompose(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;BEGIN
 definition:=pg_get_functiondef('photo.claim_apply_build()'::regprocedure);
 needle:='(b.phase=''mask'' AND NOT r.graph_sealed)';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'EDITOR_INITIAL_CLAIM_ANCHOR';END IF;
 EXECUTE replace(definition,needle,'((b.phase=''mask'' OR b.phase=''editor_precompose'' AND b.graph_version=4 AND b.stage_index IS NULL AND EXISTS(SELECT FROM photo.editor_effect_builds e WHERE e.tenant_id=b.tenant_id AND e.run_id=b.run_id)) AND NOT r.graph_sealed)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
