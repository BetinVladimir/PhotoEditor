-- +goose Up
-- Historical acknowledgement for a committed reflection graph. A missing
-- answer is deliberately indistinguishable from revoked current authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_reflection_admission_ticket(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_project uuid,p_snapshot uuid,p_proposal text,
 p_profile text,p_target_pixel jsonb,p_protected_pixel jsonb) RETURNS jsonb
 LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 plan jsonb;body jsonb;task jsonb;origin jsonb;intent jsonb;original record;
 d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_project IS NULL OR p_snapshot IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_proposal IS NULL OR octet_length(p_proposal) NOT BETWEEN 2 AND 65536
 OR p_profile IS NULL OR octet_length(p_profile) NOT BETWEEN 2 AND 65536
 OR photo.v25_layer_ref_shape(p_target_pixel) IS DISTINCT FROM true
 OR photo.v25_layer_ref_shape(p_protected_pixel) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND kind='v2' AND created_by=p_actor AND graph_sealed
  AND NOT cancel_requested AND input_snapshot_id=p_snapshot FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS DISTINCT FROM p_session THEN RETURN NULL;END IF;
 plan:=r.v28_restoration_descriptor_jcs::jsonb;
 body:=plan->'params';task:=plan->'task';origin:=plan->'origin';
 IF plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR task->>'operation' IS DISTINCT FROM 'image.reflection'
 OR task->>'operation_version' IS DISTINCT FROM '1'
 OR task->>'resource_class' IS DISTINCT FROM 'gpu'
 OR body IS DISTINCT FROM p_proposal::jsonb
 OR task->'execution_profile' IS DISTINCT FROM p_profile::jsonb
 OR origin->'target_pixel' IS DISTINCT FROM p_target_pixel
 OR origin->'protected_pixel' IS DISTINCT FROM p_protected_pixel
 THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
  AND input_snapshot_id=p_snapshot AND workflow_type='preview'
  AND renderer_version='v2.2' AND project_id=p_project FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 intent:=r.config_snapshot->'v28_restoration';
 IF intent->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR intent->>'operation' IS DISTINCT FROM 'image.reflection'
 OR intent->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR intent->>'project_id' IS DISTINCT FROM p_project::text
 OR intent->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR intent->>'actor_id' IS DISTINCT FROM p_actor::text
 OR intent->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR intent->>'run_id' IS DISTINCT FROM p_run::text
 OR intent->>'workflow_id' IS DISTINCT FROM w.id::text
 OR intent->>'ordinal' IS DISTINCT FROM split_part(task->>'node_key',':',2)
 OR intent->>'params_proposal_sha' IS DISTINCT FROM
    encode(sha256(convert_to(p_proposal,'UTF8')),'hex')
 OR intent->>'base_asset_version_id' IS DISTINCT FROM body->'base'->>'asset_version_id'
 OR intent->'target_pixel' IS DISTINCT FROM p_target_pixel
 OR intent->'protected_pixel' IS DISTINCT FROM p_protected_pixel
 OR intent->'qualification' IS DISTINCT FROM
    task->'execution_profile'->'restoration_qualification'
 OR w.policy_sha IS DISTINCT FROM intent->'qualification'->>'policy_sha'
 THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)
 THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=(task->>'id')::uuid FOR SHARE;
 IF NOT FOUND OR t.operation IS DISTINCT FROM 'image.reflection'
 OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'gpu' OR t.cancel_requested
 OR t.node_key IS DISTINCT FROM task->>'node_key'
 OR t.id IS DISTINCT FROM photo.semantic_uuid_v5(w.id,t.node_key)
 OR t.params_artifact_id::text IS DISTINCT FROM task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM task->>'input_digest'
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
    IS DISTINCT FROM task->'execution_profile'
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
 OR photo.v28_restoration_inventory_digest_ready(t.id,t.params_artifact_id,
  task->>'params_sha',t.operation,t.input_digest,plan->'entries',body,
  task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 IF plan->'entries' IS DISTINCT FROM
  (SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object(
   'artifact_id',artifact_id,'sha256',sha256),'contract',contract) ORDER BY ordinal)
   FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id
    AND contract NOT IN('InputClosureChunk','InputClosureManifest'))
 THEN RETURN NULL;END IF;
 SELECT doc.* INTO d FROM photo.backend_documents doc JOIN photo.artifacts a
  ON a.tenant_id=doc.tenant_id AND a.id=doc.artifact_id
 WHERE doc.tenant_id=p_tenant AND doc.run_id=p_run AND doc.owner_id=t.id
  AND doc.purpose='task_params' AND doc.state='ready'
  AND doc.artifact_id=t.params_artifact_id AND doc.sha256=task->>'params_sha'
  AND doc.bytes=octet_length(p_proposal) AND doc.bytes BETWEEN 2 AND 65536
  AND a.sha256=doc.sha256 AND a.bytes=doc.bytes AND a.bucket=doc.bucket
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role='task_params'
  AND a.object_version=doc.object_version AND a.object_version NOT IN('','null')
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||doc.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF doc,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO original FROM photo.v28_manual_pair_origin(p_tenant,p_project,
  p_snapshot,p_actor,p_session,body->'base',
  (body->'mask'->>'artifact_id')::uuid,body->'mask'->>'sha256',
  (body->'protected'->>'artifact_id')::uuid,body->'protected'->>'sha256',
  (p_target_pixel->>'artifact_id')::uuid,p_target_pixel->>'sha256',
  (p_protected_pixel->>'artifact_id')::uuid,p_protected_pixel->>'sha256');
 IF NOT FOUND OR original.root_id::text IS DISTINCT FROM origin->'root'->>'artifact_id'
 OR original.root_sha IS DISTINCT FROM origin->'root'->>'sha256'
 OR original.params_id::text IS DISTINCT FROM origin->'params'->>'artifact_id'
 OR original.params_sha IS DISTINCT FROM origin->'params'->>'sha256'
 OR photo.lock_v28_restoration_qualification(p_tenant,p_project,p_actor,
  p_session,'image.reflection',task->'execution_profile'->'restoration_qualification')
    IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run_id',p_run,'workflow_id',w.id,
  'task_id',t.id,'state','queued');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_reflection_admission_ticket(uuid,uuid,uuid,uuid,
 uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_reflection_admission_ticket(uuid,uuid,uuid,uuid,
 uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_reflection_admission_ticket(uuid,uuid,uuid,
 uuid,uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
