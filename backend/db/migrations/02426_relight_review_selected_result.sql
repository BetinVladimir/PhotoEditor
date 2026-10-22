-- +goose Up
-- Read the decided route from the exact adopted relight result and its preview.
-- +goose StatementBegin
CREATE FUNCTION photo.get_v28_relight_review_selection(
 p_tenant uuid,p_actor uuid,p_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,v.decision_hash,v.decision_route,
  CASE v.decision_route WHEN 'candidate' THEN candidate.id WHEN 'base' THEN base.id END,
  CASE v.decision_route WHEN 'candidate' THEN candidate.sha256 WHEN 'base' THEN base.sha256 END
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
 JOIN photo.reviews v ON v.tenant_id=p_tenant AND v.id=p_review
 JOIN photo.tasks task ON task.tenant_id=v.tenant_id AND task.id=v.task_id AND task.run_id=v.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=task.tenant_id AND workflow.run_id=task.run_id
 JOIN photo.projects project ON project.tenant_id=workflow.tenant_id AND project.id=workflow.project_id
 CROSS JOIN LATERAL photo.v28_relight_derivative_source(p_tenant,task.id) source
 JOIN photo.artifacts candidate ON candidate.tenant_id=v.tenant_id AND candidate.id=v.candidate_artifact_id
 JOIN photo.artifacts base ON base.tenant_id=v.tenant_id AND base.id=v.base_artifact_id
 JOIN photo.runs preview ON preview.tenant_id=v.tenant_id AND preview.v26_derivative_task_id=task.id
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND authority.role IN('owner','admin','editor') AND project.archived_at IS NULL
  AND (authority.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
   SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=project.id
    AND acl.user_id=p_actor AND acl.role IN('owner','admin','editor')))
  AND v.kind='image.relight' AND v.state='decided' AND v.revision=2
  AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL
  AND v.decision_hash ~ '^[0-9a-f]{64}$' AND v.candidate_hash=candidate.sha256
  AND ((v.decision='accept' AND v.decision_route='candidate') OR
   (v.decision='reject' AND v.decision_route='base') OR
   (v.decision='skip' AND v.decision_route='skip'))
  AND v.selection=jsonb_build_object('version','candidate_review_v1',
   'candidate',jsonb_build_object('artifact_id',candidate.id::text,'sha256',candidate.sha256),
   'base',jsonb_build_object('artifact_id',base.id::text,'sha256',base.sha256),'revision',2)
  AND source.run_id=v.run_id
  AND task.operation='image.relight' AND task.operation_version='1'
  AND preview.kind='preview' AND preview.status='completed' AND preview.graph_sealed
  AND NOT preview.cancel_requested
  AND preview.config_snapshot->>'source_result_id'=source.result_id::text
  AND preview.config_snapshot->>'source_attempt'=source.attempt::text
  AND preview.config_snapshot->>'source_fence'=source.fence::text
  AND preview.config_snapshot->'root'=jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha)
  AND (v.decision_route<>'base' OR photo.v28_relight_base_preview_ready(
   p_tenant,p_review,preview.config_snapshot->>'srgb_sha256'))
  AND (SELECT count(DISTINCT pt.node_key) FROM photo.tasks pt
   JOIN photo.backend_documents d ON d.tenant_id=pt.tenant_id AND d.owner_id=pt.id
    AND d.run_id=pt.run_id AND d.artifact_id=pt.params_artifact_id
    AND d.purpose='task_params' AND d.state='ready'
   WHERE pt.tenant_id=p_tenant AND pt.run_id=preview.id
    AND pt.operation='media.preview' AND pt.operation_version='1'
    AND pt.state='succeeded' AND NOT pt.cancel_requested
    AND pt.node_key IN('render.derivative:preview1600','render.derivative:preview2560')
    AND d.v26_derivative_receipt->'manifest'->'source'=
     jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256)
    AND d.v26_derivative_receipt->>'result_id'=pt.canonical_result_id::text
    AND d.v26_derivative_receipt->>'attempt'=pt.attempt::text
    AND d.v26_derivative_receipt->>'fence'=pt.fencing_token::text)=2
  AND candidate.state='ready' AND candidate.verified_at IS NOT NULL
  AND base.state='ready' AND base.verified_at IS NOT NULL
  AND candidate.object_version NOT IN('','null') AND base.object_version NOT IN('','null')
  AND (candidate.expires_at IS NULL OR candidate.expires_at>clock_timestamp())
  AND (base.expires_at IS NULL OR base.expires_at>clock_timestamp())
  AND (SELECT count(*) FROM photo.v2_workflows singleton WHERE singleton.tenant_id=p_tenant
   AND singleton.run_id=v.run_id)=1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
