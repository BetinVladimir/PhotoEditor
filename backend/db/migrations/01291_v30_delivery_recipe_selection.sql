-- +goose Up
-- Validate an explicit pinned recipe selection; never choose a latest recipe.
-- +goose StatementBegin
CREATE FUNCTION photo.resolve_v30_delivery_selection(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_recipes jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE items jsonb;total integer;si record;recipe jsonb;entry jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_recipes) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_recipes) NOT BETWEEN 1 AND 5000
 OR photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT item_count INTO total FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 IF total IS DISTINCT FROM jsonb_array_length(p_recipes) THEN RETURN NULL;END IF;
 items:='[]'::jsonb;
 FOR si IN SELECT i.* FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot AND v.review_state='accepted' ORDER BY i.ordinal FOR SHARE OF i,v LOOP
  recipe:=p_recipes->si.ordinal;
  IF jsonb_typeof(recipe) IS DISTINCT FROM 'object' OR recipe-'artifact_id'-'sha256'<>'{}'::jsonb
   OR coalesce(recipe->>'sha256','') !~ '^[0-9a-f]{64}$'
   OR photo.v26_render_recipe_scope(p_tenant,p_project,si.asset_version_id,(recipe->>'artifact_id')::uuid,recipe->>'sha256',p_actor,p_session) IS DISTINCT FROM true
   OR coalesce(si.payload->>'canvas_hash',si.payload->'binding'->>'canvas_hash','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
  entry:=jsonb_build_object('ordinal',si.ordinal,'binding',jsonb_build_object('asset_version_id',si.asset_version_id,
   'source',jsonb_build_object('artifact_id',si.artifact_id,'sha256',si.sha256),
   'canvas_hash',coalesce(si.payload->>'canvas_hash',si.payload->'binding'->>'canvas_hash')),
   'working_image',jsonb_build_object('artifact_id',si.artifact_id,'sha256',si.sha256),'recipe',recipe,
   'captured_at',si.payload->'captured_at','camera_hash',si.payload->'camera_hash');
  items:=items||jsonb_build_array(entry);
 END LOOP;
 IF jsonb_array_length(items)<>total THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('snapshot_id',p_snapshot,'items',items);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.resolve_v30_delivery_selection(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.resolve_v30_delivery_selection(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.resolve_v30_delivery_selection(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
