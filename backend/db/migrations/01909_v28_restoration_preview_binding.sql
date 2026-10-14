-- +goose Up
-- Read-only scheduler observation for a physically adopted, pending V28 review.
-- Draft intent, publication and seal must independently replay this binding.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_source(p_tenant uuid,p_task uuid)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,review_id uuid,
 result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint,
 actor_id uuid,auth_session_id uuid,source_version_id uuid,
 source_id uuid,source_sha text,canvas_hash text,candidate_id uuid,candidate_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,r.id,w.id,v.id,z.id,root.id,root.sha256,
  t.attempt,t.fencing_token,r.created_by,r.v28_restoration_session_id,
  source_version.id,base.id,base.sha256,
  (r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->>'canvas_hash',
  candidate.id,candidate.sha256
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 JOIN photo.projects project ON project.tenant_id=w.tenant_id
  AND project.id=w.project_id AND project.archived_at IS NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id
  AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.reviews v ON v.tenant_id=t.tenant_id AND v.task_id=t.id
  AND v.run_id=r.id AND v.kind=t.operation AND v.state='pending'
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id
  AND root.id::text=r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root'
  AND root.sha256=r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root_sha'
 JOIN photo.artifacts candidate ON candidate.tenant_id=t.tenant_id
  AND candidate.id=v.candidate_artifact_id AND candidate.sha256=v.candidate_hash
 JOIN photo.asset_versions source_version ON source_version.tenant_id=t.tenant_id
  AND source_version.id::text=(r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->>'asset_version_id'
 JOIN photo.assets asset ON asset.tenant_id=source_version.tenant_id
  AND asset.id=source_version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts base ON base.tenant_id=t.tenant_id
  AND base.id::text=(r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'artifact_id'
  AND base.sha256=(r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'->>'sha256'
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND t.tenant_id=p_tenant AND t.id=p_task
  AND t.operation IN('portrait.glare','image.reflection') AND t.operation_version='1'
  AND t.state='succeeded' AND NOT t.cancel_requested
  AND r.kind='v2' AND r.status='waiting_review' AND r.graph_sealed
  AND NOT r.cancel_requested AND r.v28_restoration_descriptor_jcs IS NOT NULL
  AND r.v28_restoration_session_id IS NOT NULL
  AND r.v28_restoration_adoption_receipt->>'task'=t.id::text
  AND r.v28_restoration_adoption_receipt->>'result'=z.id::text
  AND r.v28_restoration_adoption_receipt->>'attempt'=t.attempt::text
  AND r.v28_restoration_adoption_receipt->>'fence'=t.fencing_token::text
  AND r.v28_restoration_adoption_receipt->'evidence'->>'operation'=t.operation
  AND r.v28_restoration_adoption_receipt->'evidence'->'candidate'->'image'=
   jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256)
  AND r.v28_restoration_adoption_receipt->'evidence'->'candidate'->'result'=
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256)
  AND z.review_required AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
  AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt
  AND root.produced_fence=t.fencing_token AND root.output_role='document'
  AND root.media_type='application/json' AND root.state='ready'
  AND root.verified_at IS NOT NULL AND root.object_version NOT IN('','null')
  AND candidate.created_by_task_id=t.id AND candidate.produced_attempt=t.attempt
  AND candidate.produced_fence=t.fencing_token AND candidate.output_role='working_image'
  AND candidate.media_type='application/json' AND candidate.state='ready'
  AND candidate.verified_at IS NOT NULL AND candidate.object_version NOT IN('','null')
  AND base.media_type='application/json' AND base.state='ready'
  AND base.verified_at IS NOT NULL AND base.object_version NOT IN('','null')
  AND source_version.review_state='accepted'
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
  AND (base.expires_at IS NULL OR base.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id
   AND i.task_id=t.id AND i.artifact_id=base.id AND i.sha256=base.sha256
   AND i.contract='WorkingImage')
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
   AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
  AND photo.lock_v28_restoration_qualification(p_tenant,w.project_id,r.created_by,
   r.v28_restoration_session_id,t.operation,t.execution_profile->'restoration_qualification')
   IS TRUE;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_source(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_source(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
