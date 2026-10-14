-- +goose Up
-- Resolve authorized catalog intent only. Does not pin, reserve or dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.preflight_v30_delivery_create(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_presets uuid[]) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.snapshots%ROWTYPE;configs jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_presets IS NULL OR cardinality(p_presets) NOT BETWEEN 1 AND 16
 OR array_position(p_presets,NULL) IS NOT NULL
 OR (SELECT count(DISTINCT x) FROM unnest(p_presets) x)<>cardinality(p_presets)
 OR photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR SHARE;
 IF NOT FOUND OR s.item_count<>(SELECT count(*) FROM photo.snapshot_items si JOIN photo.asset_versions v
  ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id AND v.review_state='accepted'
  WHERE si.tenant_id=p_tenant AND si.snapshot_id=p_snapshot) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('id',e.id,'revision',e.revision::text,'sha256',e.config_hash,'config',e.config) ORDER BY ids.ordinality)
 INTO configs FROM unnest(p_presets) WITH ORDINALITY ids(id,ordinality)
 JOIN photo.export_presets e ON e.tenant_id=p_tenant AND e.id=ids.id AND e.archived_at IS NULL;
 IF configs IS NULL OR jsonb_array_length(configs)<>cardinality(p_presets) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('snapshot_id',s.id,'snapshot_digest',s.digest,'item_count',s.item_count,'presets',configs);
END $$;
ALTER FUNCTION photo.preflight_v30_delivery_create(uuid,uuid,uuid,uuid,uuid,uuid[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.preflight_v30_delivery_create(uuid,uuid,uuid,uuid,uuid,uuid[]) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.preflight_v30_delivery_create(uuid,uuid,uuid,uuid,uuid,uuid[]) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
