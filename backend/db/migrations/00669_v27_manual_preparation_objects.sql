-- +goose Up
-- Pre-producer manual catalog gate: no dependency on an existing defect result.
-- Original content/pixel/qualification verification remains mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_manual_preparation_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_mask uuid,p_mask_sha text)
RETURNS TABLE(root_id uuid,root_sha text,root_bucket text,root_key text,root_version text,root_bytes bigint,params_id uuid,params_sha text,params_bucket text,params_key text,params_version text,params_bytes bigint,pixel_bucket text,pixel_key text,pixel_version text,pixel_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT root.id,root.sha256,root.bucket,root.object_key,root.object_version,root.bytes,
 params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.bytes,
 pixel.bucket,pixel.object_key,pixel.object_version,pixel.bytes
 FROM photo.artifacts pixel
 JOIN photo.tasks producer ON producer.tenant_id=pixel.tenant_id AND producer.id=pixel.created_by_task_id
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.asset_versions version
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.projects project ON project.tenant_id=asset.tenant_id AND project.id=asset.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=version.tenant_id AND tenant.status='active'
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=version.master_artifact_id
 WHERE version.tenant_id=p_tenant AND version.id=p_version AND version.review_state='accepted'
 AND version.master_artifact_id=p_source AND version.content_hash=p_source_sha
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.sha256=p_source_sha AND source.state='ready' AND source.verified_at IS NOT NULL
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576)
 AND pixel.tenant_id=p_tenant AND pixel.id=p_mask AND pixel.sha256=p_mask_sha
 AND producer.state='succeeded' AND producer.operation_version='1' AND producer.resource_class='cpu'
 AND producer.operation='mask.rasterize' AND pixel.output_role='mask_target' AND root.output_role='rasterized_mask'
 AND producer.execution_profile->>'backend'='cpu' AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu'
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pixel.id,'sha256',pixel.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 AND root.media_type='application/json' AND params.media_type='application/json' AND pixel.media_type='application/octet-stream'
 AND root.bytes BETWEEN 2 AND 1048576 AND params.bytes BETWEEN 2 AND 1048576 AND pixel.bytes BETWEEN 32 AND 800065548
 AND NOT EXISTS(SELECT 1 FROM (VALUES(root.state,root.verified_at,root.object_version,root.expires_at),(params.state,params.verified_at,params.object_version,params.expires_at),(pixel.state,pixel.verified_at,pixel.object_version,pixel.expires_at)) objects(state,verified,version,expires)
 WHERE state<>'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
 AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
 AND pixel.produced_attempt=producer.attempt AND pixel.produced_fence=producer.fencing_token
 ORDER BY root.id LIMIT 2;
$$;
ALTER FUNCTION photo.semantic_proposal_manual_preparation_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_manual_preparation_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_manual_preparation_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
