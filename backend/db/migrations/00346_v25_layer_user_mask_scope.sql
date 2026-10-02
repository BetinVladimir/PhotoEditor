-- +goose Up
-- Version-scoped user upload authority for an actual composition mask root.
-- JSON type, geometry and binary probabilities still require pinned byte checks.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_user_mask_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_params jsonb,p_mask uuid,p_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v25_layer_original_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_params)
  OR p_mask IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 -- The validated expression has at most 256 occurrences and depth 16.
 IF NOT EXISTS(WITH RECURSIVE nodes(node) AS (
  SELECT p_params->'mask'
  UNION ALL SELECT c.child FROM nodes n CROSS JOIN LATERAL
   (VALUES(n.node->'left'),(n.node->'right'),(n.node->'operand')) c(child)
   WHERE c.child IS NOT NULL)
  SELECT 1 FROM nodes WHERE node->>'op'='ref'
   AND node->'mask'=jsonb_build_object('artifact_id',p_mask::text,'sha256',p_sha)
  UNION ALL SELECT 1 WHERE p_params->'protected_mask'=jsonb_build_object('artifact_id',p_mask::text,'sha256',p_sha)
 ) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
  JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id AND a.sha256=u.sha256
  WHERE u.tenant_id=p_tenant AND u.project_id=p_project
   AND u.asset_version_id=(p_params->'base'->>'asset_version_id')::uuid
   AND u.artifact_id=p_mask AND u.sha256=p_sha AND u.state='ready' AND u.completed_at IS NOT NULL
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
   AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_version=u.object_version
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_user_mask_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_user_mask_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_user_mask_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
