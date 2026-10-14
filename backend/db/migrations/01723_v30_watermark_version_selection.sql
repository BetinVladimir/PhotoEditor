-- +goose Up
-- Freeze source identity before object IO. Retries never reselect latest.
ALTER TABLE photo.runs ADD COLUMN v30_watermark_selections jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT v30_watermark_selections_bound CHECK(jsonb_typeof(v30_watermark_selections)='object' AND pg_column_size(v30_watermark_selections)<=65536);
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_watermark_selections() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
DECLARE item record;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v30_watermark_selections<>'{}'::jsonb THEN RAISE EXCEPTION 'WATERMARK_SELECTION_REQUIRES_EXISTING_RUN' USING ERRCODE='42501';END IF;
 ELSIF NEW.v30_watermark_selections IS DISTINCT FROM OLD.v30_watermark_selections THEN
  IF current_user<>'photo_fn_owner' THEN RAISE EXCEPTION 'WATERMARK_SELECTION_REQUIRES_TRUSTED_FUNCTION' USING ERRCODE='42501';END IF;
  FOR item IN SELECT * FROM jsonb_each(OLD.v30_watermark_selections) LOOP
   IF NEW.v30_watermark_selections->item.key IS DISTINCT FROM item.value THEN RAISE EXCEPTION 'IMMUTABLE_WATERMARK_SELECTION' USING ERRCODE='55000';END IF;
  END LOOP;
 END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(NEW.v30_watermark_selections))>16 THEN RAISE EXCEPTION 'WATERMARK_SELECTION_COUNT' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_watermark_selections() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_watermark_selections() FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE TRIGGER immutable_v30_watermark_selections BEFORE INSERT OR UPDATE OF v30_watermark_selections ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_watermark_selections();
CREATE FUNCTION photo.select_v30_watermark_version(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_preset uuid,p_preset_sha text,p_key text,p_request_sha text)
RETURNS TABLE(asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;r photo.runs%ROWTYPE;frozen jsonb;cfg jsonb;asset uuid;version uuid;entry jsonb;source record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR p_preset IS NULL OR p_preset_sha IS NULL OR p_preset_sha !~ '^[0-9a-f]{64}$'
 OR p_run IS DISTINCT FROM photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key) THEN RETURN;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL THEN RETURN;END IF;
 SELECT (value->>'body_jcs')::jsonb INTO frozen FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_input';
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(frozen->'presets') WHERE value->>'preset_id'=p_preset::text AND value->>'preset_sha'=p_preset_sha) THEN RETURN;END IF;
 SELECT (value->>'body_jcs')::jsonb INTO cfg FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_preset' AND value->'ref'->>'sha256'=p_preset_sha;
 asset:=(cfg->'watermark'->>'asset_id')::uuid;IF asset IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.created_by IS DISTINCT FROM p_actor OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.project_id=p_project AND w.workflow_type='delivery' AND w.renderer_version='v2.2') THEN RETURN;END IF;
 entry:=r.v30_watermark_selections->p_preset::text;
 IF entry IS NULL THEN
  -- Latest accepted version is considered once. No fallback to an older ready version.
  SELECT v.id INTO version FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.asset_id=asset AND v.review_state='accepted' ORDER BY v.version_no DESC LIMIT 1;
 ELSE
  IF entry->>'asset_id' IS DISTINCT FROM asset::text OR entry->>'preset_sha256' IS DISTINCT FROM p_preset_sha THEN RETURN;END IF;
  version:=(entry->>'asset_version_id')::uuid;
 END IF;
 SELECT * INTO source FROM photo.v30_delivery_watermark_source(p_tenant,p_project,asset,version,p_actor,p_session);
 IF NOT FOUND THEN RETURN;END IF;
 IF entry IS NULL THEN
  entry:=jsonb_build_object('asset_id',asset,'asset_version_id',source.asset_version_id,'artifact_id',source.artifact_id,'sha256',source.sha256,'preset_sha256',p_preset_sha);
  UPDATE photo.runs SET v30_watermark_selections=v30_watermark_selections||jsonb_build_object(p_preset::text,entry) WHERE tenant_id=p_tenant AND id=p_run;
 ELSIF entry->>'artifact_id' IS DISTINCT FROM source.artifact_id::text OR entry->>'sha256' IS DISTINCT FROM source.sha256 THEN RETURN;
 END IF;
 RETURN QUERY SELECT asset,source.asset_version_id,source.artifact_id,source.sha256;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.select_v30_watermark_version(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.select_v30_watermark_version(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.select_v30_watermark_version(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
