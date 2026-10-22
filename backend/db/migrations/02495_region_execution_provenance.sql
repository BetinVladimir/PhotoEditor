-- +goose Up
-- +goose StatementBegin
-- Read-only server selection. This inventory is NOT a Region admission proof;
-- callers still verify physical documents, effective Style and stage policy.
CREATE FUNCTION photo.v26_region_execution_candidates(p_tenant uuid,p_asset uuid,p_version uuid,p_source uuid,p_source_sha text,p_style uuid,p_style_sha text,p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;source_record record;candidate record;result jsonb:='[]';inventory jsonb;candidate_count integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_source_sha IS NULL OR p_style_sha IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO source_record FROM photo.v26_region_source(p_tenant,p_asset,p_version,p_actor,p_session);
 IF NOT FOUND THEN RETURN NULL;END IF;
 -- Named metadata authority remains separately rechecked by Region admission.
 SELECT a.project_id INTO project FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=p_asset AND v.id=p_version AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha AND v.review_state='accepted'
 AND a.deleted_at IS NULL AND a.upload_state='verified';
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN RETURN NULL;END IF;
 FOR candidate IN
 SELECT DISTINCT r.id,v.master_artifact_id,v.content_hash FROM photo.runs r JOIN photo.asset_versions v ON v.tenant_id=r.tenant_id AND v.created_by_run_id=r.id
 WHERE r.tenant_id=p_tenant AND v.asset_id=p_asset AND v.review_state='accepted'
 AND r.status IN('completed','completed_with_errors') AND NOT r.cancel_requested
 AND r.config_snapshot->>'project_id'=project::text
 AND r.config_snapshot->>'style_artifact_id'=p_style::text AND r.config_snapshot->>'style_hash'=p_style_sha
 AND photo.v30_canonical_image_ancestor(p_tenant,project,v.master_artifact_id,v.content_hash,p_source,p_source_sha)
 ORDER BY r.id,v.master_artifact_id,v.content_hash LIMIT 17
 LOOP
  candidate_count:=candidate_count+1;IF candidate_count>16 THEN RETURN NULL;END IF;
  SELECT jsonb_agg(entry ORDER BY task_id) INTO inventory FROM (
   SELECT t.id task_id,jsonb_build_object('task_id',t.id,'result_id',tr.id,'operation',t.operation,'operation_version',t.operation_version,
    'attempt',t.attempt,'fencing_token',t.fencing_token,'execution_profile',t.execution_profile,'phase',node.phase,
    'review_authority',CASE WHEN t.operation IN('defects.repair','sensor.dust_repair') THEN photo.v26_region_execution_repair_review(p_tenant,t.run_id,t.id)
     ELSE (SELECT photo.v26_region_execution_defect_base(p_tenant,t.run_id,a.id,a.sha256) FROM jsonb_array_elements(tr.outputs) ref JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256' WHERE a.output_role='working_image') END,
    'params',jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256),
    'params_object',jsonb_build_object('bucket',pa.bucket,'object_key',pa.object_key,'object_version',pa.object_version,'bytes',pa.bytes,'media_type',pa.media_type),
    'outputs',tr.outputs,'inputs',t.inputs) entry
   FROM photo.tasks t
   LEFT JOIN photo.apply_nodes node ON node.tenant_id=t.tenant_id AND node.id=t.id AND node.run_id=t.run_id AND node.operation=t.operation
   JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id AND tr.task_id=t.id AND tr.attempt=t.attempt AND tr.fencing_token=t.fencing_token
   JOIN photo.task_attempts att ON att.tenant_id=t.tenant_id AND att.task_id=t.id AND att.attempt=t.attempt AND att.fencing_token=t.fencing_token AND att.finished_at IS NOT NULL
   JOIN photo.artifacts pa ON pa.tenant_id=t.tenant_id AND pa.id=t.params_artifact_id AND pa.state='ready' AND pa.verified_at IS NOT NULL AND pa.media_type='application/json' AND pa.object_version NOT IN('','null') AND pa.bytes BETWEEN 2 AND 1048576 AND (pa.expires_at IS NULL OR pa.expires_at>clock_timestamp())
   WHERE t.tenant_id=p_tenant AND t.run_id=candidate.id AND t.state='succeeded' AND NOT t.cancel_requested
   AND EXISTS(SELECT FROM jsonb_array_elements(tr.outputs) ref JOIN photo.artifacts output ON output.tenant_id=t.tenant_id AND output.id=(ref->>'artifact_id')::uuid AND output.sha256=ref->>'sha256'
    WHERE output.output_role='working_image' AND output.state='ready' AND output.verified_at IS NOT NULL AND output.object_version NOT IN('','null') AND output.produced_attempt=t.attempt AND output.produced_fence=t.fencing_token AND (output.expires_at IS NULL OR output.expires_at>clock_timestamp()))
  ) entries;
  IF inventory IS NULL OR jsonb_array_length(inventory)>128 THEN CONTINUE;END IF;
  result:=result||jsonb_build_array(jsonb_build_object('run_id',candidate.id,'target',jsonb_build_object('artifact_id',candidate.master_artifact_id,'sha256',candidate.content_hash),'stages',inventory,
   'culling_authority',photo.v26_region_execution_culling(p_tenant,candidate.id,p_asset,p_version,p_source,p_source_sha)));
 END LOOP;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_candidates(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_candidates(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_execution_candidates(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid) TO photo_api;
-- Private bounded producer-input traversal. Only server-derived stage roots
-- reach this helper; it is metadata authority, not typed/physical validation.
CREATE FUNCTION photo.v26_region_execution_dependency_scope(p_tenant uuid,p_project uuid,p_roots jsonb,p_artifact uuid,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE queue jsonb:=p_roots;visited jsonb:='[]';ref jsonb;children jsonb;cursor integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR jsonb_typeof(queue) IS DISTINCT FROM 'array' OR p_sha IS NULL THEN RETURN false;END IF;
 SELECT coalesce(jsonb_agg(DISTINCT value),'[]'::jsonb) INTO queue FROM jsonb_array_elements(queue);
 WHILE cursor<jsonb_array_length(queue) LOOP
  IF jsonb_array_length(queue)>512 THEN RETURN false;END IF;
  ref:=queue->cursor;cursor:=cursor+1;
  IF visited @> jsonb_build_array(ref) THEN CONTINUE;END IF;
  visited:=visited||jsonb_build_array(ref);
  IF ref=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha) THEN RETURN true;END IF;
  SELECT t.inputs||jsonb_build_array(jsonb_build_object('artifact_id',params.id,'sha256',params.sha256)) INTO children FROM photo.artifacts a
  JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
  JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id AND tr.task_id=t.id AND tr.attempt=t.attempt AND tr.fencing_token=t.fencing_token
  JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
  JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
  JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=r.tenant_id AND workflow.run_id=r.id
  WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND t.state='succeeded' AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.status IN('completed','completed_with_errors')
  AND (r.config_snapshot->>'project_id'=p_project::text OR workflow.project_id=p_project)
  AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
  IF FOUND THEN
   IF jsonb_typeof(children) IS DISTINCT FROM 'array' OR jsonb_array_length(children)>512 THEN RETURN false;END IF;
   SELECT coalesce(jsonb_agg(value),'[]'::jsonb) INTO children FROM jsonb_array_elements(children) WHERE NOT (visited @> jsonb_build_array(value)) AND NOT (queue @> jsonb_build_array(value));
   queue:=queue||children;
  END IF;
 END LOOP;
 RETURN false;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_region_execution_dependency_scope(uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_dependency_scope(uuid,uuid,jsonb,uuid,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.v26_region_execution_document(p_tenant uuid,p_asset uuid,p_version uuid,p_source uuid,p_source_sha text,p_style uuid,p_style_sha text,p_actor uuid,p_session uuid,p_run uuid,p_task uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(project_id uuid,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text,output_role text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidates jsonb;stage jsonb;candidate jsonb;project uuid;roots jsonb;outpaint_plan jsonb;review_roots jsonb;
BEGIN
 IF p_sha IS NULL OR p_artifact IS NULL THEN RETURN;END IF;
 candidates:=photo.v26_region_execution_candidates(p_tenant,p_asset,p_version,p_source,p_source_sha,p_style,p_style_sha,p_actor,p_session);
 IF candidates IS NULL THEN RETURN;END IF;
 SELECT value INTO candidate FROM jsonb_array_elements(candidates) WHERE value->>'run_id'=p_run::text;
 IF candidate IS NULL THEN RETURN;END IF;
 SELECT value INTO stage FROM jsonb_array_elements(candidate->'stages') WHERE value->>'task_id'=p_task::text;
 IF stage IS NULL THEN RETURN;END IF;
 SELECT a.project_id INTO project FROM photo.assets a WHERE a.tenant_id=p_tenant AND a.id=p_asset;
 roots:=stage->'inputs'||stage->'outputs'||jsonb_build_array(stage->'params');
 IF stage#>>'{review_authority,route}'='base' THEN
  -- Authority was projected from the exact current no-op branch, not client
  -- JSON. Its canonical proposal producer owns labels and dependency inputs.
  SELECT parent.inputs||result.outputs||jsonb_build_array(jsonb_build_object('artifact_id',params.id,'sha256',params.sha256)) INTO review_roots
  FROM photo.tasks parent JOIN photo.task_results result ON result.tenant_id=parent.tenant_id AND result.id=parent.canonical_result_id AND result.task_id=parent.id AND result.attempt=parent.attempt AND result.fencing_token=parent.fencing_token
  JOIN photo.artifacts params ON params.tenant_id=parent.tenant_id AND params.id=parent.params_artifact_id
  WHERE parent.tenant_id=p_tenant AND parent.run_id=p_run AND parent.id=(stage#>>'{review_authority,producer_task_id}')::uuid;
  IF review_roots IS NULL THEN RETURN;END IF;
  roots:=roots||review_roots;
 END IF;
 IF stage->>'operation'='image.outpaint' THEN
  outpaint_plan:=photo.v26_region_execution_outpaint_plan(p_tenant,p_asset,p_version,p_source,p_source_sha,p_style,p_style_sha,p_actor,p_session,p_run,p_task);
  IF outpaint_plan IS NULL THEN RETURN;END IF;
  roots:=roots||jsonb_build_array(outpaint_plan);
 END IF;
 IF NOT photo.v26_region_execution_dependency_scope(p_tenant,project,roots,p_artifact,p_sha) THEN RETURN;END IF;
 RETURN QUERY SELECT owner.project_id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type,a.output_role
 FROM photo.artifacts a JOIN photo.assets owner ON owner.tenant_id=a.tenant_id AND owner.id=p_asset
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
ALTER FUNCTION photo.v26_region_execution_document(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_document(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_execution_document(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- Outpaint Params intentionally omit the proposal. Resolve its one exact
-- canonical composition-plan prerequisite from the server-authored Apply DAG.
CREATE FUNCTION photo.v26_region_execution_outpaint_plan(p_tenant uuid,p_asset uuid,p_version uuid,p_source uuid,p_source_sha text,p_style uuid,p_style_sha text,p_actor uuid,p_session uuid,p_run uuid,p_task uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidates jsonb;plans jsonb;
BEGIN
 candidates:=photo.v26_region_execution_candidates(p_tenant,p_asset,p_version,p_source,p_source_sha,p_style,p_style_sha,p_actor,p_session);
 IF candidates IS NULL OR NOT EXISTS(SELECT FROM jsonb_array_elements(candidates) c,jsonb_array_elements(c->'stages') s WHERE c->>'run_id'=p_run::text AND s->>'task_id'=p_task::text AND s->>'operation'='image.outpaint' AND s->>'phase'='composition_render') THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) INTO plans
 FROM photo.apply_nodes consumer
 JOIN photo.tasks producer ON producer.tenant_id=consumer.tenant_id AND producer.id=ANY(consumer.prerequisites) AND producer.run_id=consumer.run_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 CROSS JOIN LATERAL jsonb_array_elements(result.outputs) ref
 JOIN photo.artifacts a ON a.tenant_id=producer.tenant_id AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
 WHERE consumer.tenant_id=p_tenant AND consumer.run_id=p_run AND consumer.id=p_task AND consumer.phase='composition_render' AND consumer.operation='image.outpaint'
 AND producer.operation='composition.plan' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND a.output_role='composition_plan' AND a.created_by_task_id=producer.id AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF jsonb_array_length(plans) IS DISTINCT FROM 1 THEN RETURN NULL;END IF;
 RETURN plans->0;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_outpaint_plan(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_outpaint_plan(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_execution_outpaint_plan(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid) TO photo_api;
-- The producer selector accepts only an already authorized dependency of the
-- exact current stage; callers cannot name an unrelated composition proposal.
CREATE FUNCTION photo.v26_region_execution_producer_params(p_tenant uuid,p_asset uuid,p_version uuid,p_source uuid,p_source_sha text,p_style uuid,p_style_sha text,p_actor uuid,p_session uuid,p_run uuid,p_task uuid,p_plan uuid,p_plan_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scoped record;result jsonb;
BEGIN
 SELECT * INTO scoped FROM photo.v26_region_execution_document(p_tenant,p_asset,p_version,p_source,p_source_sha,p_style,p_style_sha,p_actor,p_session,p_run,p_task,p_plan,p_plan_sha);
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256) INTO result
 FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id AND tr.task_id=t.id AND tr.attempt=t.attempt AND tr.fencing_token=t.fencing_token
 JOIN photo.task_attempts att ON att.tenant_id=t.tenant_id AND att.task_id=t.id AND att.attempt=t.attempt AND att.fencing_token=t.fencing_token AND att.finished_at IS NOT NULL
 JOIN photo.artifacts pa ON pa.tenant_id=t.tenant_id AND pa.id=t.params_artifact_id
 WHERE a.tenant_id=p_tenant AND a.id=p_plan AND a.sha256=p_plan_sha AND t.operation='composition.plan' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND pa.state='ready' AND pa.verified_at IS NOT NULL AND pa.media_type='application/json' AND pa.object_version NOT IN('','null') AND pa.bytes BETWEEN 2 AND 1048576 AND (pa.expires_at IS NULL OR pa.expires_at>clock_timestamp());
 RETURN result;
END $$;
ALTER FUNCTION photo.v26_region_execution_producer_params(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_producer_params(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_execution_producer_params(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- Exact portable asset plus its canonical decode producer. This is deliberately
-- narrower than the dependency walk: declaring an arbitrary decoded image is
-- insufficient to claim that it is the accepted Style background asset.
CREATE FUNCTION photo.v26_region_execution_background(p_tenant uuid,p_asset uuid,p_version uuid,p_source uuid,p_source_sha text,p_style uuid,p_style_sha text,p_actor uuid,p_session uuid,p_run uuid,p_task uuid,p_decoded uuid,p_decoded_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scoped record;pin jsonb;result jsonb;project uuid;
BEGIN
 SELECT * INTO scoped FROM photo.v26_region_execution_document(p_tenant,p_asset,p_version,p_source,p_source_sha,p_style,p_style_sha,p_actor,p_session,p_run,p_task,p_decoded,p_decoded_sha);
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT r.config_snapshot->'background_asset',a.project_id INTO pin,project
 FROM photo.runs r JOIN photo.assets a ON a.tenant_id=r.tenant_id AND a.id=p_asset
 JOIN photo.tasks consumer ON consumer.tenant_id=r.tenant_id AND consumer.id=p_task AND consumer.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND consumer.operation='background.compose'
 AND consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_decoded,'sha256',p_decoded_sha));
 IF pin IS NULL OR pin->'style' IS DISTINCT FROM jsonb_build_object('artifact_id',p_style,'sha256',p_style_sha) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT FROM photo.styles s
 JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
 JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
 JOIN photo.artifacts original ON original.tenant_id=b.tenant_id AND original.id=b.artifact_id AND original.sha256=b.sha256
 WHERE s.tenant_id=p_tenant AND s.project_id=project AND s.archived_at IS NULL
 AND v.id=(pin->>'style_version_id')::uuid AND v.approval_status='accepted' AND v.export_artifact_id=p_style AND v.config_hash=p_style_sha
 AND v.config_json#>>'{extensions,background,mode}'='asset' AND v.config_json#>>'{extensions,background,asset_id}'=pin->>'portable_asset_id'
 AND b.portable_asset_id=pin->>'portable_asset_id' AND pin->'source'=jsonb_build_object('artifact_id',original.id,'sha256',original.sha256)
 AND (original.output_role='style_asset' OR (original.output_role='v30_import_prepared' AND photo.v30_registered_style_background(p_tenant,project,v.id,original.id,original.sha256,b.portable_asset_id,p_actor,p_session) IS TRUE))
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.object_version NOT IN('','null')
 AND original.bytes BETWEEN 1 AND 536870912 AND original.bytes=(pin->>'expected_bytes')::bigint
 AND original.media_type IN('image/jpeg','image/png','image/tiff') AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('asset',pin,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256)) INTO result
 FROM photo.artifacts decoded JOIN photo.tasks producer ON producer.tenant_id=decoded.tenant_id AND producer.id=decoded.created_by_task_id
 JOIN photo.task_results tr ON tr.tenant_id=producer.tenant_id AND tr.id=producer.canonical_result_id AND tr.task_id=producer.id AND tr.attempt=producer.attempt AND tr.fencing_token=producer.fencing_token
 JOIN photo.task_attempts att ON att.tenant_id=producer.tenant_id AND att.task_id=producer.id AND att.attempt=producer.attempt AND att.fencing_token=producer.fencing_token AND att.finished_at IS NOT NULL
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 WHERE decoded.tenant_id=p_tenant AND decoded.id=p_decoded AND decoded.sha256=p_decoded_sha AND decoded.output_role='working_image'
 AND producer.run_id=p_run AND producer.operation='asset.decode' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND decoded.produced_attempt=producer.attempt AND decoded.produced_fence=producer.fencing_token
 AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',decoded.id,'sha256',decoded.sha256))
 AND producer.inputs @> jsonb_build_array(pin->'source')
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
 AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp());
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_background(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_background(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_execution_background(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- Current human-decision authority is separate from a hash-valid review body.
-- The returned selection is repeated against physical Params/review documents
-- by worker and adopter. No candidate acceptance is inferred from task success.
CREATE FUNCTION photo.v26_region_execution_repair_review(p_tenant uuid,p_run uuid,p_task uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task photo.tasks%ROWTYPE;decision photo.reviews%ROWTYPE;branch photo.defect_branches%ROWTYPE;node photo.apply_nodes%ROWTYPE;producer photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;proposal jsonb;selection jsonb;
BEGIN
 SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task AND state='succeeded' AND NOT cancel_requested;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF task.operation='defects.repair' THEN
  SELECT * INTO branch FROM photo.defect_branches WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=p_task;
  IF NOT FOUND THEN RETURN NULL;END IF;
  SELECT * INTO decision FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND id=branch.review_id AND kind='defects' AND state='decided' AND decision_route='repair';
  IF NOT FOUND OR decision.decision_hash IS DISTINCT FROM branch.payload->>'decision_hash'
   OR branch.payload#>>'{task,id}' IS DISTINCT FROM task.id::text
   OR branch.payload#>>'{task,params,artifact_id}' IS DISTINCT FROM task.params_artifact_id::text
   OR NOT(task.inputs @> jsonb_build_array(branch.payload->'selection')) THEN RETURN NULL;END IF;
  IF NOT EXISTS(SELECT FROM photo.tasks parent JOIN photo.task_results canonical ON canonical.tenant_id=parent.tenant_id AND canonical.id=parent.canonical_result_id AND canonical.task_id=parent.id AND canonical.attempt=parent.attempt AND canonical.fencing_token=parent.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=parent.tenant_id AND attempt.task_id=parent.id AND attempt.attempt=parent.attempt AND attempt.fencing_token=parent.fencing_token AND attempt.finished_at IS NOT NULL
   JOIN photo.artifacts candidate ON candidate.tenant_id=parent.tenant_id AND candidate.id=decision.candidate_artifact_id AND candidate.sha256=decision.candidate_hash
   WHERE parent.tenant_id=p_tenant AND parent.run_id=p_run AND parent.id=decision.task_id AND parent.operation='defects.propose' AND parent.state='succeeded' AND NOT parent.cancel_requested
   AND candidate.state='ready' AND candidate.verified_at IS NOT NULL AND candidate.created_by_task_id=parent.id AND candidate.produced_attempt=parent.attempt AND candidate.produced_fence=parent.fencing_token
   AND candidate.object_version NOT IN('','null') AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
   AND canonical.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',candidate.id::text,'sha256',candidate.sha256))) THEN RETURN NULL;END IF;
  IF NOT EXISTS(SELECT FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
   WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=decision.id AND d.purpose='review_selection' AND d.state='ready'
   AND a.id=(branch.payload#>>'{selection,artifact_id}')::uuid AND a.sha256=branch.payload#>>'{selection,sha256}' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('review_id',decision.id,'decision_hash',decision.decision_hash,'selection',decision.selection,'selection_ref',branch.payload->'selection');
 ELSIF task.operation='sensor.dust_repair' THEN
  SELECT * INTO node FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task AND phase='dust_repair' AND operation='sensor.dust_repair';
  IF NOT FOUND THEN RETURN NULL;END IF;
  proposal:=node.params->'proposal';selection:=node.params->'review';
  IF proposal IS NULL OR proposal='null'::jsonb OR selection IS NULL OR selection='null'::jsonb THEN RETURN NULL;END IF;
  SELECT t.* INTO producer FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
   WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=ANY(node.prerequisites) AND t.operation='sensor.dust' AND t.state='succeeded' AND NOT t.cancel_requested
   AND a.id=(proposal->>'artifact_id')::uuid AND a.sha256=proposal->>'sha256' AND a.output_role='dust_candidates'
   AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 1048576 AND a.media_type='application/json' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND EXISTS(SELECT FROM photo.task_attempts attempt WHERE attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL);
  IF NOT FOUND THEN RETURN NULL;END IF;
  SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id AND task_id=producer.id AND attempt=producer.attempt AND fencing_token=producer.fencing_token AND outputs @> jsonb_build_array(proposal);
  IF NOT FOUND THEN RETURN NULL;END IF;
  SELECT * INTO decision FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=producer.id AND kind='sensor_dust' AND state='decided';
  IF FOUND THEN
   IF decision.selection IS DISTINCT FROM selection OR decision.candidate_artifact_id::text IS DISTINCT FROM proposal->>'artifact_id' OR decision.candidate_hash IS DISTINCT FROM proposal->>'sha256' THEN RETURN NULL;END IF;
   RETURN jsonb_build_object('review_id',decision.id,'decision_hash',decision.decision_hash,'selection',selection);
  END IF;
  -- A producer with no review can only authorize the exact empty continuation
  -- emitted by apply_v4_dust_decisions; it never accepts proposed candidates.
  IF result.review_required OR EXISTS(SELECT FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=producer.id AND kind='sensor_dust')
   OR selection IS DISTINCT FROM jsonb_build_object('version','sensor_dust_review_v1','proposal',proposal,'base',(SELECT params->'frames'->0 FROM photo.apply_nodes WHERE tenant_id=p_tenant AND id=producer.id AND run_id=p_run),'revision',1,'accepted_candidate_ids','[]'::jsonb,'rejected_candidate_ids','[]'::jsonb) THEN RETURN NULL;END IF;
  RETURN jsonb_build_object('selection',selection);
 END IF;
 RETURN NULL;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_repair_review(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_repair_review(uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;

-- A photographer choosing base authorizes an explicit identity at this exact
-- point in the image DAG. Do not invent a repair task for the no-op branch.
CREATE FUNCTION photo.v26_region_execution_defect_base(p_tenant uuid,p_run uuid,p_base uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proofs jsonb;
BEGIN
 SELECT jsonb_agg(jsonb_build_object('route','base','review_id',v.id,'decision_hash',v.decision_hash,'selection',v.selection,'producer_task_id',parent.id)) INTO proofs
 FROM photo.defect_branches b JOIN photo.reviews v ON v.tenant_id=b.tenant_id AND v.id=b.review_id AND v.run_id=b.run_id AND v.kind='defects' AND v.state='decided' AND v.decision_route='base'
 JOIN photo.tasks parent ON parent.tenant_id=v.tenant_id AND parent.id=v.task_id AND parent.run_id=v.run_id AND parent.operation='defects.propose' AND parent.state='succeeded' AND NOT parent.cancel_requested
 JOIN photo.task_results result ON result.tenant_id=parent.tenant_id AND result.id=parent.canonical_result_id AND result.task_id=parent.id AND result.attempt=parent.attempt AND result.fencing_token=parent.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=parent.tenant_id AND attempt.task_id=parent.id AND attempt.attempt=parent.attempt AND attempt.fencing_token=parent.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts candidate ON candidate.tenant_id=parent.tenant_id AND candidate.id=v.candidate_artifact_id AND candidate.sha256=v.candidate_hash AND candidate.created_by_task_id=parent.id AND candidate.produced_attempt=parent.attempt AND candidate.produced_fence=parent.fencing_token
 JOIN photo.artifacts base ON base.tenant_id=b.tenant_id AND base.id=b.base_artifact_id AND base.id=p_base AND base.sha256=p_sha
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.task_id IS NULL AND b.payload->>'route'='base' AND b.payload->>'decision_hash'=v.decision_hash
 AND b.payload->'base'=jsonb_build_object('artifact_id',p_base::text,'sha256',p_sha) AND v.selection->'image'=b.payload->'base'
 AND base.state='ready' AND base.verified_at IS NOT NULL AND base.object_version NOT IN('','null') AND (base.expires_at IS NULL OR base.expires_at>clock_timestamp())
 AND candidate.state='ready' AND candidate.verified_at IS NOT NULL AND candidate.object_version NOT IN('','null') AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',candidate.id::text,'sha256',candidate.sha256));
 IF jsonb_array_length(proofs) IS DISTINCT FROM 1 THEN RETURN NULL;END IF;
 RETURN proofs->0;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_defect_base(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_defect_base(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

-- Culling has collection membership semantics, not a single-image pixel
-- filter. Region preserves the frozen photographer selection; it cannot rerank
-- or auto-select a new collection while previewing one member.
CREATE FUNCTION photo.v26_region_execution_culling(p_tenant uuid,p_run uuid,p_asset uuid,p_version uuid,p_source uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proofs jsonb;
BEGIN
 SELECT jsonb_agg(jsonb_build_object('version','region_culling_selection_v1','run_id',r.id,'review_id',v.id,'decision_hash',v.decision_hash,
  'snapshot_id',s.id,'snapshot_digest',s.digest,'item_id',item.item_id,'producer_task_id',t.id,'never_delete',true)) INTO proofs
 FROM photo.runs r JOIN photo.apply_culling_reviews c ON c.tenant_id=r.tenant_id AND c.run_id=r.id AND c.snapshot_id=r.input_snapshot_id AND c.snapshot_digest=r.config_snapshot->>'input_snapshot_digest'
 JOIN photo.snapshots s ON s.tenant_id=c.tenant_id AND s.id=c.snapshot_id AND s.digest=c.snapshot_digest
 JOIN photo.snapshot_items item ON item.tenant_id=s.tenant_id AND item.snapshot_id=s.id AND item.asset_version_id=p_version AND item.artifact_id=p_source AND item.sha256=p_sha
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id AND version.id=item.asset_version_id AND version.asset_id=p_asset
 JOIN photo.reviews v ON v.tenant_id=c.tenant_id AND v.id=c.review_id AND v.run_id=r.id AND v.kind='culling' AND v.state='decided' AND v.decision='accept' AND v.decision_route='selection'
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=r.id AND t.operation='collection.cull' AND t.state='succeeded' AND NOT t.cancel_requested
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id AND result.task_id=t.id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=v.candidate_artifact_id AND a.sha256=v.candidate_hash AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND NOT r.cancel_requested AND r.status IN('completed','completed_with_errors')
 AND v.selection->'never_delete'='true'::jsonb AND v.selection->'selected_item_ids' @> jsonb_build_array(item.item_id::text)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF jsonb_array_length(proofs) IS DISTINCT FROM 1 THEN RETURN NULL;END IF;
 RETURN proofs->0;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v26_region_execution_culling(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_culling(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.v26_region_execution_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_params jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE execution jsonb;candidates jsonb;candidate jsonb;stage jsonb;actual jsonb;current_ref jsonb;selected_run text;found_run text;
BEGIN
 IF NOT (p_params ? 'execution') THEN RETURN true;END IF;
 execution:=p_params->'execution';
 IF execution->>'schema_version' IS DISTINCT FROM '1' OR execution->'source' IS DISTINCT FROM p_params#>'{source,ref}'
 OR execution->'style' IS DISTINCT FROM p_params#>'{frozen_recipe,base_style}'
 OR execution->>'source_canvas_hash' IS DISTINCT FROM p_params#>>'{frozen_recipe,source,canvas_hash}'
 OR jsonb_typeof(execution->'stages') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(execution->'stages') NOT BETWEEN 1 AND 128 THEN RETURN false;END IF;
 candidates:=photo.v26_region_execution_candidates(p_tenant,(p_params->>'asset_id')::uuid,(p_params#>>'{request,version_id}')::uuid,
 (execution#>>'{source,artifact_id}')::uuid,execution#>>'{source,sha256}',(execution#>>'{style,artifact_id}')::uuid,execution#>>'{style,sha256}',p_actor,p_session);
 IF candidates IS NULL THEN RETURN false;END IF;
 current_ref:=execution->'source';
 FOR stage IN SELECT value FROM jsonb_array_elements(execution->'stages') LOOP
  IF stage->'source' IS DISTINCT FROM current_ref THEN RETURN false;END IF;
  actual:=NULL;found_run:=NULL;
  FOR candidate IN SELECT value FROM jsonb_array_elements(candidates) LOOP
   IF selected_run IS NOT NULL AND candidate->>'run_id'<>selected_run THEN CONTINUE;END IF;
   SELECT value INTO actual FROM jsonb_array_elements(candidate->'stages') WHERE value->>'task_id'=stage->>'task_id';
   IF actual IS NOT NULL THEN found_run:=candidate->>'run_id';EXIT;END IF;
  END LOOP;
  IF actual IS NULL OR actual->>'result_id' IS DISTINCT FROM stage->>'result_id'
  OR actual->>'operation' IS DISTINCT FROM stage->>'operation' OR actual->>'operation_version' IS DISTINCT FROM stage->>'operation_version'
  OR actual->>'attempt' IS DISTINCT FROM stage->>'attempt' OR actual->>'fencing_token' IS DISTINCT FROM stage->>'fencing_token'
  OR actual->'params' IS DISTINCT FROM stage->'params'
  OR coalesce(actual->'review_authority','null'::jsonb) IS DISTINCT FROM coalesce(stage->'review_authority','null'::jsonb)
  OR (stage ? 'phase' AND actual->>'phase' IS DISTINCT FROM stage->>'phase')
  OR (stage->>'operation'='image.generate' AND (stage->>'phase' IS NULL OR stage->>'phase' NOT IN('background','generate')))
  OR (stage->>'operation'='image.outpaint' AND stage->>'phase' IS DISTINCT FROM 'composition_render')
  OR NOT (actual->'inputs' @> jsonb_build_array(stage->'source')) OR NOT (actual->'outputs' @> jsonb_build_array(stage->'output')) THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'params')
  OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'source' AND declared->>'contract'='WorkingImage')
  OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'output' AND declared->>'contract'='WorkingImage') THEN RETURN false;END IF;
  IF stage->>'operation'='geometry.body' AND NOT (stage ? 'report') THEN RETURN false;END IF;
  IF stage->>'operation' IN('defects.repair','sensor.dust_repair') THEN
   IF NOT(stage ? 'review_authority') OR photo.v26_region_execution_repair_review(p_tenant,found_run::uuid,(stage->>'task_id')::uuid) IS DISTINCT FROM stage->'review_authority' THEN RETURN false;END IF;
  ELSIF stage ? 'review_authority' THEN
   IF photo.v26_region_execution_defect_base(p_tenant,found_run::uuid,(stage#>>'{output,artifact_id}')::uuid,stage#>>'{output,sha256}') IS DISTINCT FROM stage->'review_authority' THEN RETURN false;END IF;
   IF NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage#>'{review_authority,selection,proposal}' AND declared->>'contract'='DefectCandidates')
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage#>'{review_authority,selection,label_map}' AND declared->>'contract'='NpyArray')
    OR stage#>'{review_authority,selection,image}' IS DISTINCT FROM stage->'output' THEN RETURN false;END IF;
  END IF;
  IF (stage ? 'background_asset') IS DISTINCT FROM (stage ? 'background_params') OR (stage ? 'background_asset') IS DISTINCT FROM (stage ? 'background_image') OR (stage ? 'background_asset') IS DISTINCT FROM (stage ? 'background_canvas') THEN RETURN false;END IF;
  IF stage ? 'background_asset' THEN
   IF stage->>'operation' IS DISTINCT FROM 'background.compose'
    OR NOT(actual->'inputs' @> jsonb_build_array(stage->'background_image'))
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'background_image' AND declared->>'contract'='WorkingImage')
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'background_params' AND declared->>'contract'='Params')
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage#>'{background_asset,source}' AND declared->>'contract'='EncodedImage') THEN RETURN false;END IF;
   IF photo.v26_region_execution_background(p_tenant,(p_params->>'asset_id')::uuid,(p_params#>>'{request,version_id}')::uuid,
    (execution#>>'{source,artifact_id}')::uuid,execution#>>'{source,sha256}',(execution#>>'{style,artifact_id}')::uuid,execution#>>'{style,sha256}',p_actor,p_session,
    found_run::uuid,(stage->>'task_id')::uuid,(stage#>>'{background_image,artifact_id}')::uuid,stage#>>'{background_image,sha256}')
    IS DISTINCT FROM jsonb_build_object('asset',stage->'background_asset','params',stage->'background_params') THEN RETURN false;END IF;
  END IF;
  IF stage ? 'report' AND (NOT (actual->'outputs' @> jsonb_build_array(stage->'report'))
   OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'report')) THEN RETURN false;END IF;
  IF stage->>'operation' IN('geometry.crop','image.outpaint') THEN
   IF NOT (stage ? 'plan') OR NOT (stage ? 'policy_params')
    OR (stage->>'operation'='geometry.crop' AND NOT (actual->'inputs' @> jsonb_build_array(stage->'plan')))
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'plan' AND declared->>'contract'='CompositionPlan')
    OR NOT EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE declared->'ref'=stage->'policy_params' AND declared->>'contract'='Params') THEN RETURN false;END IF;
   IF stage->>'operation'='image.outpaint' AND photo.v26_region_execution_outpaint_plan(p_tenant,(p_params->>'asset_id')::uuid,(p_params#>>'{request,version_id}')::uuid,
    (execution#>>'{source,artifact_id}')::uuid,execution#>>'{source,sha256}',(execution#>>'{style,artifact_id}')::uuid,execution#>>'{style,sha256}',p_actor,p_session,
    found_run::uuid,(stage->>'task_id')::uuid) IS DISTINCT FROM stage->'plan' THEN RETURN false;END IF;
   IF photo.v26_region_execution_producer_params(p_tenant,(p_params->>'asset_id')::uuid,(p_params#>>'{request,version_id}')::uuid,
    (execution#>>'{source,artifact_id}')::uuid,execution#>>'{source,sha256}',(execution#>>'{style,artifact_id}')::uuid,execution#>>'{style,sha256}',p_actor,p_session,
    found_run::uuid,(stage->>'task_id')::uuid,(stage#>>'{plan,artifact_id}')::uuid,stage#>>'{plan,sha256}') IS DISTINCT FROM stage->'policy_params' THEN RETURN false;END IF;
  END IF;
  selected_run:=found_run;current_ref:=stage->'output';
 END LOOP;
 RETURN EXISTS(SELECT FROM jsonb_array_elements(candidates) candidate WHERE candidate->>'run_id'=selected_run AND candidate->'target'=current_ref
  AND coalesce(candidate->'culling_authority','null'::jsonb)=coalesce(execution->'culling_authority','null'::jsonb));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_region_execution_scope(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_execution_scope(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
-- Every existing activation/worker/adoption path already invokes locked scope.
ALTER FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) RENAME TO v26_region_locked_scope_pre_execution;
REVOKE ALL ON FUNCTION photo.v26_region_locked_scope_pre_execution(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.v26_region_locked_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_params jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer_tasks uuid[];producer_runs uuid[];
BEGIN
 -- Existing callers acquire tenant -> own run -> own task. Reacquire tenant
 -- explicitly, then protect completed upstream producers before the final
 -- authority read. Immutable result payloads do not freeze cancellation, GC or
 -- the task's current canonical-result pointer. Tenant serialization prevents
 -- two Region callers with already-held own-run locks reversing run order.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_params ? 'execution' THEN
  IF jsonb_typeof(p_params->'inputs') IS DISTINCT FROM 'array' OR jsonb_typeof(p_params#>'{execution,stages}') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF jsonb_array_length(p_params->'inputs')>511 OR jsonb_array_length(p_params#>'{execution,stages}') NOT BETWEEN 1 AND 128 THEN RETURN false;END IF;
  SELECT array_agg(t.id ORDER BY t.id),array_agg(DISTINCT t.run_id ORDER BY t.run_id) INTO producer_tasks,producer_runs
  FROM photo.tasks t WHERE t.tenant_id=p_tenant AND
   (t.id=(p_params#>>'{execution,culling_authority,producer_task_id}')::uuid
    OR EXISTS(SELECT FROM jsonb_array_elements(p_params#>'{execution,stages}') stage WHERE (stage->>'task_id')::uuid=t.id OR (stage#>>'{review_authority,producer_task_id}')::uuid=t.id)
    OR EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(declared#>>'{ref,artifact_id}')::uuid AND a.sha256=declared#>>'{ref,sha256}' WHERE a.created_by_task_id=t.id));
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=ANY(producer_runs) ORDER BY id FOR SHARE;
  PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(producer_tasks) ORDER BY id FOR SHARE;
  -- Phase and prerequisite edges are server-authored insert-only inventory
  -- (photo_fn_owner has SELECT/INSERT, no UPDATE); still lock the exact rows
  -- while rechecking the descriptor so maintenance cannot change its meaning.
  PERFORM id FROM photo.apply_nodes WHERE tenant_id=p_tenant AND id=ANY(producer_tasks) ORDER BY id FOR SHARE;
  PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=ANY(producer_runs) ORDER BY id FOR SHARE;
  PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=ANY(producer_tasks) ORDER BY task_id,attempt,fencing_token FOR SHARE;
  PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=ANY(producer_tasks) ORDER BY id FOR SHARE;
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND (EXISTS(SELECT FROM jsonb_array_elements(p_params->'inputs') declared WHERE (declared#>>'{ref,artifact_id}')::uuid=a.id AND declared#>>'{ref,sha256}'=a.sha256)
    OR EXISTS(SELECT FROM photo.reviews review WHERE review.tenant_id=p_tenant AND review.task_id=ANY(producer_tasks) AND review.candidate_artifact_id=a.id)) ORDER BY a.id FOR SHARE;
 END IF;
 IF NOT photo.v26_region_locked_scope_pre_execution(p_tenant,p_actor,p_session,p_params) THEN RETURN false;END IF;
 RETURN photo.v26_region_execution_scope(p_tenant,p_actor,p_session,p_params);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_locked_scope(uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- Replay profile is returned only for the same current canonical result whose
-- Params and full Region authority are admitted by the existing accessor.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_adoption_execution_profile(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.execution_profile FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task
 AND EXISTS(SELECT FROM photo.v26_region_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result));
$$;
ALTER FUNCTION photo.v26_region_adoption_execution_profile(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_adoption_execution_profile(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v26_region_adoption_execution_profile(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
