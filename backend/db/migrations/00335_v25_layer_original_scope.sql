-- +goose Up
-- Exact original-source provenance predicate for null composition lineage.
-- Parent byte/canvas validation and atomic graph locks remain separate duties.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_params jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE base jsonb;
BEGIN
 IF NOT photo.v25_layer_params_shape(p_params)
  OR p_params->'source_result' IS DISTINCT FROM 'null'::jsonb
  OR NOT photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) THEN RETURN false; END IF;
 base:=p_params->'base';
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items i
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  JOIN photo.artifacts source ON source.tenant_id=i.tenant_id AND source.id=i.artifact_id AND source.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot
   AND i.asset_version_id=(base->>'asset_version_id')::uuid
   AND i.artifact_id=(base->'source'->>'artifact_id')::uuid AND i.sha256=base->'source'->>'sha256'
   AND v.review_state='accepted' AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
   AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
   AND source.output_role='working_image' AND source.media_type='application/json');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_scope(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_scope(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_scope(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
