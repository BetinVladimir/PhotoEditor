-- +goose Up
-- Metadata only: does not prove physical bytes, replacement membership or
-- admission. A caller must verify original objects and repeat/lock this lookup.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_adopted_optical_parent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;matches integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL
 OR photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT count(*),min(jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_project,
 'task_id',t.id,'run_id',r.id,'result_id',z.id,'attempt',t.attempt,'fencing_token',t.fencing_token,
 'operation',t.operation,'source',jsonb_build_object('artifact_id',image.id,'sha256',image.sha256,
 'bucket',image.bucket,'object_key',image.object_key,'object_version',image.object_version,
 'bytes',image.bytes,'media_type',image.media_type))::text)::jsonb INTO matches,result
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=r.id AND w.project_id=p_project
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.v2_artifact_links root_link ON root_link.tenant_id=w.tenant_id AND root_link.workflow_id=w.id
 AND root_link.project_id=w.project_id AND root_link.ordinal=0
 AND root_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id=root_link.artifact_id AND root.sha256=root_link.sha256
 JOIN photo.v2_artifact_links image_link ON image_link.tenant_id=w.tenant_id AND image_link.workflow_id=w.id
 AND image_link.project_id=w.project_id AND image_link.ordinal=0
 AND image_link.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
 JOIN photo.artifacts image ON image.tenant_id=t.tenant_id AND image.id=image_link.artifact_id AND image.sha256=image_link.sha256
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.run_id=p_run
 AND t.operation IN('optical.negative','optical.perspective','optical.panorama','optical.focus_stack') AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.kind='v2' AND r.graph_sealed
 AND r.status IN('running','waiting_review','completed')
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND image.created_by_task_id=t.id AND image.produced_attempt=t.attempt AND image.produced_fence=t.fencing_token
 AND root.state='ready' AND image.state='ready' AND root.verified_at IS NOT NULL AND image.verified_at IS NOT NULL
 AND root.object_version NOT IN('','null') AND image.object_version NOT IN('','null')
 AND root.media_type='application/json' AND root.output_role='document'
 AND image.media_type='application/json' AND image.output_role='working_image' AND image.bytes BETWEEN 2 AND 65536
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations invalid WHERE invalid.tenant_id=t.tenant_id AND invalid.task_id=t.id)
 -- Only negative has a qualified selected-review authority here. Other review
 -- operations must gain their own exact authority before becoming sources.
 AND (NOT z.review_required OR (t.operation='optical.negative' AND EXISTS(
 SELECT 1 FROM photo.v29_negative_review_selection(p_tenant,t.id) s
 WHERE s.route='candidate' AND s.image_id=image.id AND s.image_sha=image.sha256)));
 IF matches<>1 THEN RETURN NULL;END IF;
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
