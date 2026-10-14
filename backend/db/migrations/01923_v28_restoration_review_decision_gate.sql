-- +goose Up
-- Tenant-scoped live precondition for a restoration decision. The caller must
-- hold editor/project authority and repeat this gate in the decision TX.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_review_decision_ready(p_tenant uuid,p_review uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.reviews v
  JOIN photo.tasks origin ON origin.tenant_id=v.tenant_id AND origin.id=v.task_id
   AND origin.run_id=v.run_id AND origin.operation=v.kind
  JOIN photo.runs parent ON parent.tenant_id=v.tenant_id AND parent.id=v.run_id
  CROSS JOIN LATERAL photo.v28_restoration_preview_source(p_tenant,origin.id) s
  JOIN photo.runs preview ON preview.tenant_id=v.tenant_id
   AND preview.v28_restoration_preview_task_id=origin.id
   AND preview.config_snapshot->>'source_result_id'=s.result_id::text
   AND preview.config_snapshot->>'source_review_id'=v.id::text
  WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
   AND v.tenant_id=p_tenant AND v.id=p_review AND v.state='pending'
   AND v.kind IN('portrait.glare','image.reflection') AND s.review_id=v.id
   AND v.candidate_artifact_id=s.candidate_id AND v.candidate_hash=s.candidate_sha
   AND v.base_artifact_id=s.source_id
   AND parent.status='waiting_review' AND parent.graph_sealed AND NOT parent.cancel_requested
   AND parent.v28_restoration_adoption_receipt->>'task'=origin.id::text
   AND parent.v28_restoration_adoption_receipt->>'result'=s.result_id::text
   AND preview.kind='preview' AND preview.status='completed' AND preview.graph_sealed
   AND NOT preview.cancel_requested
   AND preview.config_snapshot->>'source_attempt'=s.attempt::text
   AND preview.config_snapshot->>'source_fence'=s.fence::text
   AND preview.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
   AND preview.config_snapshot->'before'=jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
   AND preview.config_snapshot->'after'=jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
   AND (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=p_tenant
    AND t.run_id=preview.id)=4
   AND (SELECT count(DISTINCT t.node_key) FROM photo.tasks t JOIN photo.backend_documents d
    ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
     AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params'
    WHERE t.tenant_id=p_tenant AND t.run_id=preview.id
     AND t.state='succeeded' AND NOT t.cancel_requested
     AND t.operation='media.preview' AND t.operation_version='1'
     AND d.state='ready' AND d.v28_restoration_preview_params IS NOT NULL
     AND d.v28_restoration_preview_derivative_receipt IS NOT NULL
     AND t.node_key IN('restoration.before:preview1600','restoration.before:preview2560',
      'restoration.after:preview1600','restoration.after:preview2560'))=4
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_review_decision_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_review_decision_ready(uuid,uuid)
 FROM PUBLIC,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_review_decision_ready(uuid,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
