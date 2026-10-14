-- +goose Up
-- Sessionless equivalent of the completed session.plan provenance proof,
-- gated by the exact claimed CorrectionPlan input and object version.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_correction_evidence(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_artifact uuid,p_sha text,p_body text,p_bucket text,p_object_key text,
 p_object_version text,p_bytes bigint)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;result jsonb;matches integer;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_artifact IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
  OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_sha
  OR coalesce(p_bucket,'')='' OR coalesce(p_object_key,'')=''
  OR coalesce(p_object_version,'') IN ('','null')
  OR p_bytes<>octet_length(p_body) THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_artifact,p_sha,'CorrectionPlan');
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.projects project
  JOIN photo.tenants tenant ON tenant.id=project.tenant_id
  WHERE project.tenant_id=p_tenant AND project.id=w.project_id
   AND project.archived_at IS NULL AND project.download_allowed
   AND tenant.status='active') THEN RETURN NULL;END IF;
 body:=p_body::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object'
  OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR (body-'schema_version'-'snapshot_digest'-'items')<>'{}'::jsonb
  OR body->>'snapshot_digest' IS NULL
  OR body->>'snapshot_digest' !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(body->'items') IS DISTINCT FROM 'array'
  OR jsonb_array_length(body->'items') NOT BETWEEN 1 AND 5000 THEN RETURN NULL;END IF;
 IF (SELECT count(DISTINCT item->>'ordinal') FROM jsonb_array_elements(body->'items') item)
   <>jsonb_array_length(body->'items')
  OR (SELECT count(DISTINCT item#>>'{binding,asset_version_id}')
   FROM jsonb_array_elements(body->'items') item)
   <>jsonb_array_length(body->'items') THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(candidate.evidence)->0 INTO matches,result FROM (
  SELECT jsonb_build_object('group_id',g.id,'snapshot_id',snapshot.id,
   'snapshot_digest',snapshot.digest,'policy_sha',analysis.policy_sha,
   'item_count',snapshot.item_count,
   'root',jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
   'bucket',root.bucket,'object_key',root.object_key,
   'object_version',root.object_version,'bytes',root.bytes,
   'media_type',root.media_type) AS evidence
  FROM photo.artifacts correction
  JOIN photo.tasks task ON task.tenant_id=correction.tenant_id
   AND task.id=correction.created_by_task_id
  JOIN photo.task_results z ON z.tenant_id=task.tenant_id
   AND z.id=task.canonical_result_id AND z.task_id=task.id
  JOIN photo.runs r ON r.tenant_id=task.tenant_id AND r.id=task.run_id
  JOIN photo.v2_workflows workflow ON workflow.tenant_id=r.tenant_id
   AND workflow.run_id=r.id AND workflow.project_id=w.project_id
  JOIN photo.session_analyses analysis ON analysis.tenant_id=workflow.tenant_id
   AND analysis.project_id=workflow.project_id AND analysis.workflow_id=workflow.id
  JOIN photo.snapshots snapshot ON snapshot.tenant_id=analysis.tenant_id
   AND snapshot.id=analysis.snapshot_id
  JOIN photo.scene_groups g ON g.tenant_id=analysis.tenant_id
   AND g.project_id=analysis.project_id AND g.analysis_id=analysis.id
  JOIN photo.artifacts root ON root.tenant_id=analysis.tenant_id
   AND root.id=analysis.plan_artifact_id
  WHERE correction.tenant_id=p_tenant AND correction.id=p_artifact
   AND correction.sha256=p_sha AND correction.bucket=p_bucket
   AND correction.object_key=p_object_key
   AND correction.object_version=p_object_version AND correction.bytes=p_bytes
   AND correction.state='ready' AND correction.output_role='correction_plan'
   AND correction.media_type='application/json' AND correction.verified_at IS NOT NULL
   AND (correction.expires_at IS NULL OR correction.expires_at>clock_timestamp())
   AND task.operation='session.plan' AND task.operation_version='1'
   AND task.node_key='session.plan:0' AND task.state='succeeded'
   AND NOT task.cancel_requested AND task.attempt=z.attempt
   AND task.fencing_token=z.fencing_token AND z.review_required
   AND correction.produced_attempt=z.attempt AND correction.produced_fence=z.fencing_token
   AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
   AND r.status IN('waiting_review','running','completed')
   AND workflow.workflow_type='session' AND workflow.renderer_version='v2.2'
   AND workflow.input_snapshot_id=snapshot.id AND r.input_snapshot_id=snapshot.id
   AND snapshot.kind='v2' AND snapshot.digest=body->>'snapshot_digest'
   AND root.created_by_task_id=task.id AND root.produced_attempt=z.attempt
   AND root.produced_fence=z.fencing_token AND root.output_role='document'
   AND root.state='ready' AND root.verified_at IS NOT NULL
   AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 8388608
   AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
   AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
   AND z.outputs=jsonb_build_array(
    jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
   AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
    AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(task.id,z.id))
   AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=p_tenant
    AND l.project_id=w.project_id AND l.workflow_id=workflow.id
    AND l.artifact_id=correction.id AND l.sha256=correction.sha256)
   AND (SELECT count(*) FROM photo.scene_members m WHERE m.tenant_id=p_tenant
    AND m.project_id=w.project_id AND m.group_id=g.id)=jsonb_array_length(body->'items')
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(body->'items') item
    LEFT JOIN photo.scene_members m ON m.tenant_id=p_tenant
     AND m.project_id=w.project_id AND m.group_id=g.id
     AND m.snapshot_ordinal=(item->>'ordinal')::integer
     AND m.asset_version_id=(item#>>'{binding,asset_version_id}')::uuid
    LEFT JOIN photo.snapshot_items si ON si.tenant_id=p_tenant
     AND si.snapshot_id=snapshot.id AND si.ordinal=m.snapshot_ordinal
     AND si.asset_version_id=m.asset_version_id
     AND si.artifact_id=(item#>>'{binding,source,artifact_id}')::uuid
     AND si.sha256=item#>>'{binding,source,sha256}'
    WHERE m.id IS NULL OR si.snapshot_id IS NULL)
 ) candidate;
 IF matches<>1 THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_export_adoption_correction_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_correction_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_correction_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
