-- +goose Up
-- Sealed-graph acknowledgement, not a derivative-output acceptance receipt.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_receipt(p_tenant uuid,p_run uuid,
 p_task uuid,p_review uuid,p_result uuid,p_root_sha text,p_srgb text)
RETURNS TABLE(state text,before1600 uuid,before2560 uuid,after1600 uuid,after2560 uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.status,a.id,b.id,c.id,d.id FROM photo.runs r
 CROSS JOIN LATERAL photo.v28_restoration_preview_source(p_tenant,p_task) s
 JOIN photo.tasks a ON a.tenant_id=r.tenant_id AND a.run_id=r.id
  AND a.node_key='restoration.before:preview1600'
 JOIN photo.tasks b ON b.tenant_id=r.tenant_id AND b.run_id=r.id
  AND b.node_key='restoration.before:preview2560'
 JOIN photo.tasks c ON c.tenant_id=r.tenant_id AND c.run_id=r.id
  AND c.node_key='restoration.after:preview1600'
 JOIN photo.tasks d ON d.tenant_id=r.tenant_id AND d.run_id=r.id
  AND d.node_key='restoration.after:preview2560'
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='preview' AND r.graph_sealed
  AND r.v28_restoration_preview_task_id=p_task AND s.review_id=p_review
  AND s.result_id=p_result AND s.root_sha=p_root_sha
  AND r.status IN('running','completed','completed_with_errors','failed','cancelled')
  AND r.config_snapshot->>'source_review_id'=s.review_id::text
  AND r.config_snapshot->>'source_result_id'=s.result_id::text
  AND r.config_snapshot->>'source_attempt'=s.attempt::text
  AND r.config_snapshot->>'source_fence'=s.fence::text
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  AND r.config_snapshot->'before'=jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
  AND r.config_snapshot->'after'=jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
  AND r.config_snapshot->>'srgb_sha256'=p_srgb
  AND a.operation='media.preview' AND b.operation='media.preview'
  AND c.operation='media.preview' AND d.operation='media.preview'
  AND a.operation_version='1' AND b.operation_version='1'
  AND c.operation_version='1' AND d.operation_version='1'
  AND (SELECT count(*) FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run)=4
  AND (SELECT count(*) FROM photo.backend_documents doc JOIN photo.tasks t
   ON t.tenant_id=doc.tenant_id AND t.id=doc.owner_id AND t.run_id=doc.run_id
   AND t.params_artifact_id=doc.artifact_id WHERE doc.tenant_id=p_tenant
   AND doc.run_id=p_run AND doc.purpose='task_params' AND doc.state='ready'
   AND doc.v28_restoration_preview_params IS NOT NULL)=4;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_receipt(uuid,uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_receipt(uuid,uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_receipt(uuid,uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
