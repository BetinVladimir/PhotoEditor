-- +goose Up
-- Match schema inventory array segments literally; purpose equality is unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v25_adopted_lens_child_object(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid,p_root uuid,p_root_sha text,
 p_artifact uuid,p_sha text,p_path text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v25_adopted_lens_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session) b
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=p_task
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.project_id=p_project
 JOIN photo.v2_artifact_links l ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id AND l.project_id=p_project
  AND l.artifact_id=p_artifact AND l.sha256=p_sha
  AND l.purpose=encode(sha256(convert_to('tasks/'||p_task::text||'/'||p_path,'UTF8')),'hex')
 JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
 WHERE b.artifact_id=p_root AND b.sha256=p_root_sha
  AND p_path ~ '^\$/([A-Za-z0-9_]+|\*)(/([A-Za-z0-9_]+|\*))*$' AND length(p_path) BETWEEN 3 AND 512
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.bytes>0
  AND ((p_contract='WorkingImage' AND a.output_role='working_image' AND a.media_type='application/json')
   OR (p_contract='ImageReport' AND a.output_role='report' AND a.media_type='application/json')
   OR (p_contract='MaskRef' AND a.output_role='mask' AND a.media_type='application/json')
   OR (p_contract='WorkingPixels' AND a.output_role='pixels' AND a.media_type='application/x-npy')
   OR (p_contract='WorkingAlpha' AND a.output_role='alpha' AND a.media_type='application/x-npy')
   OR (p_contract='MaskPixels' AND a.output_role='mask_pixels' AND a.media_type='application/x-npy')
   OR (p_contract='DenseWarpField' AND a.output_role='warp_field' AND a.media_type='application/x-npy'));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_adopted_lens_child_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_adopted_lens_child_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_adopted_lens_child_object(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
