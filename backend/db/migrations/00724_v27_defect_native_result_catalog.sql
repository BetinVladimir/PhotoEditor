-- +goose Up
-- Read-only canonical native result discovery; content verification stays in Go.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_native_result_catalog(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid)
RETURNS TABLE(result_id uuid,outputs jsonb,params jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;z photo.task_results%ROWTYPE;p photo.artifacts%ROWTYPE;
 ref jsonb;aid uuid;seen uuid[]:='{}';ordinal integer;
 roles text[]:=ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'];
 media text[]:=ARRAY['application/octet-stream','application/octet-stream','image/png','application/json','application/json'];
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.operation<>'defects.propose' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('running','waiting_review','completed')
 OR r.config_snapshot->'v27_defect_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_defect_prepare'->>'task_id' IS DISTINCT FROM t.id::text
 OR r.config_snapshot->'v27_defect_prepare'->>'input_digest' IS DISTINCT FROM t.input_digest
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND project_id=p_project AND workflow_type='semantic' AND renderer_version='v2.2') THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt AND fencing_token=t.fencing_token;
 IF NOT FOUND OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs)<>5 OR octet_length(z.outputs::text)>8192 THEN RETURN;END IF;
 SELECT * INTO p FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id;
 IF NOT FOUND OR r.config_snapshot->'v27_defect_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p.id,'sha256',p.sha256) THEN RETURN;END IF;
 FOR ordinal IN 0..4 LOOP
  ref:=z.outputs->ordinal;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}' OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string' OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(seen) OR aid=p.id THEN RETURN;END IF;
  seen:=array_append(seen,aid);
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
   WHERE a.tenant_id=p_tenant AND a.id=aid AND a.sha256=ref->>'sha256' AND a.output_role=roles[ordinal+1] AND a.media_type=media[ordinal+1]
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND u.task_id=t.id AND u.attempt=t.attempt AND u.fence=t.fencing_token AND u.state='ready' AND u.completed_at IS NOT NULL
   AND u.role=a.output_role AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.media_type=a.media_type) THEN RETURN;END IF;
 END LOOP;
 IF NOT photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
 (z.outputs->3->>'artifact_id')::uuid,z.outputs->3->>'sha256',(z.outputs->4->>'artifact_id')::uuid,z.outputs->4->>'sha256',
 (z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256',(z.outputs->1->>'artifact_id')::uuid,z.outputs->1->>'sha256',p.id,p.sha256) THEN RETURN;END IF;
 RETURN QUERY SELECT z.id,z.outputs,jsonb_build_object('artifact_id',p.id,'sha256',p.sha256);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.semantic_defect_native_result_catalog(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_native_result_catalog(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_native_result_catalog(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
