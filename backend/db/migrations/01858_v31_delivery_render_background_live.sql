-- +goose Up
-- Reuse the existing live portable background/style binding under the exact
-- delegated render Params; no editor session is needed for this readonly gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_background_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_asset uuid,p_asset_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_asset_sha!~'^[0-9a-f]{64}$' THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
  WHERE root.tenant_id=(source->>'tenant_id')::uuid
   AND root.project_id=(source->>'project_id')::uuid
   AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
   AND root.sha256=e->'params'->>'sha256'
   AND root.state='ready' AND root.render_registered_at IS NOT NULL
   AND root.render_background_asset IS NOT NULL
   AND root.render_background_asset->'source'->>'artifact_id'=p_asset::text
   AND root.render_background_asset->'source'->>'sha256'=p_asset_sha
   AND photo.v26_render_background_binding_ready(root.tenant_id,
    root.project_id,root.render_background_asset));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_background_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_background_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_background_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
