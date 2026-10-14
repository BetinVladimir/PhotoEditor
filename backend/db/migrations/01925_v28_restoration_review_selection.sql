-- +goose Up
-- A decided V28 review selects an image without changing the producer result.
-- This reader deliberately does not use the pending-only preview-source gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_review_selection(p_tenant uuid,p_task uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,v.decision_hash,v.decision_route,
  CASE v.decision_route WHEN 'candidate' THEN c.id WHEN 'base' THEN b.id ELSE NULL END,
  CASE v.decision_route WHEN 'candidate' THEN c.sha256 WHEN 'base' THEN b.sha256 ELSE NULL END
 FROM photo.reviews v
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id
  AND project.archived_at IS NULL
 JOIN photo.artifacts c ON c.tenant_id=v.tenant_id AND c.id=v.candidate_artifact_id
  AND c.sha256=v.candidate_hash
 JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id
  AND root.id::text=r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root'
  AND root.sha256=r.v28_restoration_adoption_receipt->'evidence'->'scope'->>'root_sha'
 JOIN photo.runs preview ON preview.tenant_id=t.tenant_id
  AND preview.v28_restoration_preview_task_id=t.id
  AND preview.config_snapshot->>'source_result_id'=z.id::text
  AND preview.config_snapshot->>'source_review_id'=v.id::text
 WHERE v.tenant_id=p_tenant AND v.task_id=p_task
  AND v.kind IN('portrait.glare','image.reflection') AND t.operation=v.kind
  AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
  AND z.review_required AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
  AND v.state='decided' AND v.revision=2 AND v.decided_by IS NOT NULL
  AND v.decided_at IS NOT NULL AND v.decision_hash ~ '^[0-9a-f]{64}$'
  AND ((v.decision='accept' AND v.decision_route='candidate') OR
   (v.decision='reject' AND v.decision_route='base') OR
   (v.decision='skip' AND v.decision_route='skip'))
  AND v.selection=jsonb_build_object('version','candidate_review_v1',
   'candidate',jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256),
   'base',jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256),'revision',2)
  AND r.v28_restoration_adoption_receipt->>'task'=t.id::text
  AND r.v28_restoration_adoption_receipt->>'result'=z.id::text
  AND r.v28_restoration_adoption_receipt->>'attempt'=t.attempt::text
  AND r.v28_restoration_adoption_receipt->>'fence'=t.fencing_token::text
  AND r.v28_restoration_adoption_receipt->'evidence'->>'operation'=t.operation
  AND r.v28_restoration_adoption_receipt->'evidence'->'candidate'->'image'=
   jsonb_build_object('artifact_id',c.id,'sha256',c.sha256)
  AND r.v28_restoration_adoption_receipt->'evidence'->'candidate'->'result'=
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256)
  AND (r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'=
   jsonb_build_object('artifact_id',b.id,'sha256',b.sha256)
  AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
  AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt
  AND root.produced_fence=t.fencing_token AND root.output_role='document'
  AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt
  AND c.produced_fence=t.fencing_token AND c.output_role='working_image'
  AND root.media_type='application/json' AND c.media_type='application/json'
  AND b.media_type='application/json'
  AND root.state='ready' AND c.state='ready' AND b.state='ready'
  AND root.verified_at IS NOT NULL AND c.verified_at IS NOT NULL
  AND b.verified_at IS NOT NULL
  AND root.object_version NOT IN('','null') AND c.object_version NOT IN('','null')
  AND b.object_version NOT IN('','null')
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
  AND (b.expires_at IS NULL OR b.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id
   AND i.task_id=t.id AND i.project_id=w.project_id AND i.artifact_id=b.id
   AND i.sha256=b.sha256 AND i.contract='WorkingImage')
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
   AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
  AND preview.kind='preview' AND preview.status='completed' AND preview.graph_sealed
  AND NOT preview.cancel_requested
  AND preview.config_snapshot->>'source_attempt'=t.attempt::text
  AND preview.config_snapshot->>'source_fence'=t.fencing_token::text
  AND preview.config_snapshot->'root'=
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256)
  AND preview.config_snapshot->'before'=
   jsonb_build_object('artifact_id',b.id,'sha256',b.sha256)
  AND preview.config_snapshot->'after'=
   jsonb_build_object('artifact_id',c.id,'sha256',c.sha256)
  AND (SELECT count(*) FROM photo.tasks pt WHERE pt.tenant_id=p_tenant
   AND pt.run_id=preview.id)=4
  AND (SELECT count(DISTINCT pt.node_key) FROM photo.tasks pt JOIN photo.backend_documents d
   ON d.tenant_id=pt.tenant_id AND d.run_id=pt.run_id AND d.owner_id=pt.id
    AND d.artifact_id=pt.params_artifact_id AND d.purpose='task_params'
   WHERE pt.tenant_id=p_tenant AND pt.run_id=preview.id AND pt.state='succeeded'
    AND NOT pt.cancel_requested AND pt.operation='media.preview'
    AND pt.operation_version='1' AND d.state='ready'
    AND d.v28_restoration_preview_params IS NOT NULL
    AND d.v28_restoration_preview_derivative_receipt IS NOT NULL
    AND pt.node_key IN('restoration.before:preview1600',
     'restoration.before:preview2560','restoration.after:preview1600',
     'restoration.after:preview2560'))=4
  AND NOT EXISTS(SELECT 1 FROM photo.reviews other WHERE other.tenant_id=t.tenant_id
   AND other.task_id=t.id AND other.id<>v.id);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_review_selection(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_review_selection(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_review_selection(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.get_v28_restoration_review_selection(
 p_tenant uuid,p_actor uuid,p_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT selected.review_id,selected.decision_hash,selected.route,
  selected.image_id,selected.image_sha
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
 JOIN photo.reviews v ON v.tenant_id=p_tenant AND v.id=p_review
  AND v.kind IN('portrait.glare','image.reflection')
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id
  AND project.archived_at IS NULL
 CROSS JOIN LATERAL photo.v28_restoration_review_selection(p_tenant,t.id) selected
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND authority.role IN('owner','admin','editor')
  AND (authority.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
   SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
    AND acl.project_id=project.id AND acl.user_id=p_actor
    AND acl.role IN('owner','admin','editor')))
  AND selected.review_id=p_review
  AND (SELECT count(*) FROM photo.v2_workflows singleton WHERE singleton.tenant_id=t.tenant_id
   AND singleton.run_id=t.run_id)=1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_v28_restoration_review_selection(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v28_restoration_review_selection(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.get_v28_restoration_review_selection(uuid,uuid,uuid,uuid)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
