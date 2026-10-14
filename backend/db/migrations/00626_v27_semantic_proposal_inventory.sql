-- +goose Up
-- Complete ordered target inventory; no partial snapshot is returned.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_inventory(p_tenant uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_canvas text,p_mask uuid,p_mask_sha text,p_snapshot uuid)
RETURNS TABLE(project_id uuid,item_count integer,ordinal integer,asset_version_id uuid,source_id uuid,source_sha text,canvas_hash text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH authorized AS MATERIALIZED (
  SELECT * FROM photo.semantic_proposal_catalog(p_tenant,p_actor,p_session,p_version,p_source,p_source_sha,p_canvas,p_mask,p_mask_sha,p_snapshot)
 ), inventory AS MATERIALIZED (
  SELECT a.project_id,a.item_count,i.ordinal,i.asset_version_id,i.artifact_id AS source_id,i.sha256 AS source_sha,i.payload->>'canvas_hash' AS canvas_hash
  FROM authorized a JOIN photo.snapshot_items i ON i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.review_state='accepted'
  WHERE jsonb_typeof(i.payload->'canvas_hash')='string' AND i.payload->>'canvas_hash' ~ '^[0-9a-f]{64}$'
 )
 SELECT * FROM inventory WHERE (SELECT count(*) FROM inventory)=inventory.item_count ORDER BY ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
