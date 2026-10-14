-- +goose Up
-- Complete current draft recipe inventory, authorized by the proposal selection.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_recipe_objects(p_tenant uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_canvas text,p_mask uuid,p_mask_sha text,p_snapshot uuid)
RETURNS TABLE(ordinal integer,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH inventory AS MATERIALIZED (
  SELECT * FROM photo.semantic_proposal_inventory(p_tenant,p_actor,p_session,p_version,p_source,p_source_sha,p_canvas,p_mask,p_mask_sha,p_snapshot)
 ), recipes AS MATERIALIZED (
  SELECT i.ordinal,i.asset_version_id,a.id AS artifact_id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
  FROM inventory i
  JOIN photo.v2_document_streams s ON s.tenant_id=p_tenant AND s.project_id=i.project_id AND s.asset_version_id=i.asset_version_id AND s.stream_type='draft'
  JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.id=s.current_document_id AND d.stream_id=s.id AND d.project_id=s.project_id AND d.document_type='EditingRecipeV3'
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
  WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document' AND a.media_type='application/json'
   AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 )
 SELECT * FROM recipes WHERE (SELECT count(*) FROM recipes)=(SELECT count(*) FROM inventory) ORDER BY ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_proposal_recipe_objects(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_recipe_objects(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_recipe_objects(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
