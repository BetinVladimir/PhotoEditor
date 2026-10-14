-- +goose Up
-- Background authority repeats V25 snapshot/source checks without borrowing a
-- user's possibly expired HTTP session. Current membership/project rights are
-- checked independently; this predicate never grants generic user access.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_source_ready(
 p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.snapshots s
 JOIN photo.tenants tenant ON tenant.id=s.tenant_id AND tenant.status='active'
 JOIN photo.projects project ON project.tenant_id=s.tenant_id AND project.id=p_project
  AND project.archived_at IS NULL AND project.download_allowed
 JOIN photo.memberships member ON member.tenant_id=s.tenant_id AND member.user_id=p_actor
  AND member.status='active' AND member.role IN('owner','admin','editor')
 JOIN photo.users actor ON actor.id=member.user_id AND actor.status='active'
 WHERE s.tenant_id=p_tenant AND s.id=p_snapshot AND s.kind='v2'
 AND s.principal_type='user' AND s.principal_id=p_actor AND s.expires_at IS NULL
 AND s.item_count BETWEEN 1 AND 5000
 AND (member.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
  SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
   AND acl.project_id=p_project AND acl.user_id=p_actor AND acl.role='editor'))
 AND (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)=s.item_count
 AND (SELECT count(DISTINCT i.asset_version_id) FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)=s.item_count
 AND (SELECT min(i.ordinal)=0 AND max(i.ordinal)=s.item_count-1 FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)
 AND (SELECT count(*) FROM photo.snapshot_items i
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
   AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
  JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
  JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
  JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
  WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
   AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
   AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
   AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
   AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
   AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
   AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
   AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp()))=s.item_count);
$$;
ALTER FUNCTION photo.v30_export_adoption_source_ready(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_source_ready(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;

-- Only the first canonical output may be used as the result root. The returned
-- version is an exact object pin, not an adopted result or bundle permission.
CREATE FUNCTION photo.v30_export_adoption_root_output(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 z photo.task_results%ROWTYPE;pending jsonb;root_id uuid;root_sha text;
BEGIN
 IF photo.v30_export_adoption_claim_current(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND run_id=p_run FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR SHARE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result AND task_id=p_task FOR SHARE;
 SELECT response_body INTO pending FROM photo.idempotency_records WHERE tenant_id=p_tenant
  AND principal_type='user' AND principal_id=p_actor AND route='Interchange.Export.Pending'
  AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL
  AND expires_at>clock_timestamp() FOR SHARE;
 IF w.id IS NULL OR r.id IS NULL OR t.id IS NULL OR z.id IS NULL OR pending IS NULL
  OR w.v30_export_adoption_token IS DISTINCT FROM p_token
  OR (w.v30_export_adoption_retry_after>clock_timestamp()) IS DISTINCT FROM true
  OR r.created_by IS DISTINCT FROM p_actor
  OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha
  OR r.input_snapshot_id IS DISTINCT FROM w.input_snapshot_id
  OR pending->'frozen'->>'request_sha256' IS DISTINCT FROM p_request_sha
  OR pending->'frozen'->'identity' IS DISTINCT FROM jsonb_build_object(
   'Run',p_run,'Workflow',p_workflow,'Task',p_task,'Snapshot',w.input_snapshot_id,
   'Params',photo.v30_uuid5(p_task,'interchange:params:1'))
  OR r.status IS DISTINCT FROM 'running' OR r.cancel_requested OR NOT r.graph_sealed
  OR t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested
  OR t.canonical_result_id IS DISTINCT FROM z.id OR z.attempt IS DISTINCT FROM t.attempt
  OR z.fencing_token IS DISTINCT FROM t.fencing_token OR z.review_required
  OR t.resource_class IS DISTINCT FROM 'cpu'
  OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR t.params_artifact_id IS DISTINCT FROM (pending->'frozen'->'params'->>'artifact_id')::uuid
  OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true
  OR photo.v30_export_adoption_source_ready(p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
  OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array'
  OR jsonb_array_length(z.outputs) NOT BETWEEN 3 AND 53 THEN RETURN;END IF;
 root_id:=(z.outputs->0->>'artifact_id')::uuid;
 root_sha:=z.outputs->0->>'sha256';
 IF root_id IS NULL OR root_sha IS NULL OR root_sha !~ '^[0-9a-f]{64}$'
  OR z.outputs->0 IS DISTINCT FROM jsonb_build_object('artifact_id',root_id,'sha256',root_sha) THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=root_id AND a.sha256=root_sha
  AND a.created_by_task_id=p_task AND a.produced_attempt=z.attempt
  AND a.produced_fence=z.fencing_token AND a.output_role='document'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 2 AND 65536 AND a.media_type='application/json'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input
   WHERE input.tenant_id=p_tenant AND input.task_id=p_task AND input.artifact_id=a.id)
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_root_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_root_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_root_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
