-- +goose Up
-- Read-only producer/version proof. No generic project-scope fallback.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_adopted_lens_binding(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(result_id uuid,artifact_id uuid,sha256 text,source_artifact_id uuid,source_sha256 text,canvas_hash text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT z.id,a.id,a.sha256,si.artifact_id,si.sha256,n.node->'params'->'base'->>'canvas_hash'
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.project_id=p_project
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id
  AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id
  AND si.asset_version_id=p_version
 JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id
  AND v.master_artifact_id=si.artifact_id AND v.content_hash=si.sha256 AND v.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
  AND asset.project_id=p_project AND asset.deleted_at IS NULL
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'v25_lens'->'nodes')='array'
  THEN r.config_snapshot->'v25_lens'->'nodes' ELSE '[]'::jsonb END) n(node)
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id::text=z.outputs->0->>'artifact_id'
  AND a.sha256=z.outputs->0->>'sha256'
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
  AND t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.lens' AND t.operation_version='1'
  AND t.state='succeeded' AND NOT t.cancel_requested AND NOT r.cancel_requested
  AND r.kind='v2' AND w.renderer_version='v2.2' AND t.execution_profile->>'renderer_version'='v2.2'
  AND n.node->>'task_id'=t.id::text AND n.node->'params'->'base'->>'asset_version_id'=p_version::text
  AND n.node->'params'->'base'->'source'->>'artifact_id'=si.artifact_id::text
  AND n.node->'params'->'base'->'source'->>'sha256'=si.sha256
  AND coalesce(n.node->'params'->'base'->>'canvas_hash','') ~ '^[0-9a-f]{64}$'
  AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
  AND a.output_role='document' AND a.media_type='application/json' AND a.state='ready'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
   AND i.project_id=p_project AND i.artifact_id=si.artifact_id AND i.sha256=si.sha256 AND i.contract='WorkingImage')
  AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=w.tenant_id AND l.workflow_id=w.id
   AND l.project_id=p_project AND l.artifact_id=a.id AND l.sha256=a.sha256)
  AND EXISTS(SELECT 1 FROM photo.inbox i JOIN photo.run_events e ON e.tenant_id=i.tenant_id
   AND e.run_id=r.id AND e.type='v2.task.adopted' AND e.payload->>'task_id'=t.id::text
   AND e.payload->>'result_id'=z.id::text
   WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
    AND i.event_id=photo.v2_adoption_event_id(t.id,z.id));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_adopted_lens_binding(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_adopted_lens_binding(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_adopted_lens_binding(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
