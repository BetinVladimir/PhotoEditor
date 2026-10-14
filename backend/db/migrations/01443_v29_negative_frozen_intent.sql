-- +goose Up
-- Resolve only Params of a task in this exact persisted invalidation decision.
-- Profile/task result is deliberately excluded: qualification is recomputed.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_frozen_intent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_project,'review_id',p_review,
 'decision_hash',p_hash,'original_task_id',t.id,'original_run_id',t.run_id,'operation',t.operation,
 'params',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)) INTO result
 FROM photo.v29_negative_invalidations i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.project_id=p_project
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=a.id AND d.sha256=a.sha256
 AND d.bytes=a.bytes AND d.bucket=a.bucket AND d.object_version=a.object_version
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.review_id=p_review AND i.decision_hash=p_hash
 AND t.operation IN('optical.negative','optical.perspective','optical.panorama','optical.focus_stack') AND t.operation_version='1'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='task_params' AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 65536 AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v29_negative_review_selection(p_tenant,i.origin_task_id) selected
 WHERE selected.review_id=p_review AND selected.decision_hash=p_hash AND selected.route=i.route);
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
