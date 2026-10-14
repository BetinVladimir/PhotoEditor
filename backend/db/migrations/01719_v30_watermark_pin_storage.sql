-- +goose Up
-- Bounded run-owned pins follow existing run retention and tenant purge.
ALTER TABLE photo.runs ADD COLUMN v30_watermark_sources jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT v30_watermark_sources_bound CHECK(jsonb_typeof(v30_watermark_sources)='object' AND pg_column_size(v30_watermark_sources)<=33554432);
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_watermark_sources() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
DECLARE item record;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v30_watermark_sources<>'{}'::jsonb THEN RAISE EXCEPTION 'WATERMARK_PIN_REQUIRES_EXISTING_RUN' USING ERRCODE='42501';END IF;
 ELSIF NEW.v30_watermark_sources IS DISTINCT FROM OLD.v30_watermark_sources THEN
  IF current_user<>'photo_fn_owner' THEN RAISE EXCEPTION 'WATERMARK_PIN_REQUIRES_TRUSTED_FUNCTION' USING ERRCODE='42501';END IF;
  FOR item IN SELECT * FROM jsonb_each(OLD.v30_watermark_sources) LOOP
   IF NEW.v30_watermark_sources->item.key IS DISTINCT FROM item.value THEN RAISE EXCEPTION 'IMMUTABLE_WATERMARK_PIN' USING ERRCODE='55000';END IF;
  END LOOP;
 END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(NEW.v30_watermark_sources))>16 THEN RAISE EXCEPTION 'WATERMARK_PIN_COUNT' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_watermark_sources() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_watermark_sources() FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE TRIGGER immutable_v30_watermark_sources BEFORE INSERT OR UPDATE OF v30_watermark_sources ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_watermark_sources();
CREATE FUNCTION photo.pin_v30_watermark_source(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_raw text,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;r photo.runs%ROWTYPE;project uuid;body jsonb;scope jsonb;frozen jsonb;preset jsonb;image jsonb;array_item jsonb;entry jsonb;source record;position integer:=0;expected_ref jsonb;contract text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 2097152
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_raw,'UTF8')),'hex') THEN RETURN false;END IF;
 body:=p_raw::jsonb;scope:=body->'scope';
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(body))<>6 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(scope) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(scope))<>6
 OR scope->>'tenant_id' IS DISTINCT FROM p_tenant::text OR scope->>'actor_id' IS DISTINCT FROM p_actor::text OR scope->>'parent_run_id' IS DISTINCT FROM p_run::text
 OR p_run<>photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key) THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.workflow_type='delivery' AND w.renderer_version='v2.2';
 IF project IS NULL OR scope->>'project_id' IS DISTINCT FROM project::text OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT (value->>'body_jcs')::jsonb INTO frozen FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_input';
 SELECT value INTO preset FROM jsonb_array_elements(frozen->'presets') WHERE value->>'preset_id'=scope->>'preset_id';
 IF preset IS NULL OR preset->>'preset_sha' IS DISTINCT FROM scope->>'preset_sha256' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(saved->'documents') d WHERE d->>'purpose'='v30_delivery_preset'
 AND d->'ref'->>'sha256'=scope->>'preset_sha256'
 AND ((d->>'body_jcs')::jsonb)->'watermark'->>'asset_id'=body->>'asset_id') THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.v30_delivery_watermark_source(p_tenant,project,(body->>'asset_id')::uuid,(body->'binding'->>'asset_version_id')::uuid,p_actor,p_session);
 IF NOT FOUND OR body->'binding'->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',source.artifact_id,'sha256',source.sha256)
 OR source.sha256 IS DISTINCT FROM encode(sha256(convert_to(body->>'manifest_jcs','UTF8')),'hex') THEN RETURN false;END IF;
 image:=(body->>'manifest_jcs')::jsonb;
 IF image->'alpha' IS NULL OR image->'alpha'='null'::jsonb OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1' OR image->'color'->>'encoding' IS DISTINCT FROM 'gamma24'
 OR jsonb_typeof(body->'arrays') IS DISTINCT FROM 'array' OR jsonb_array_length(body->'arrays')<>2 THEN RETURN false;END IF;
 FOR array_item IN SELECT value FROM jsonb_array_elements(body->'arrays') LOOP
  contract:=CASE position WHEN 0 THEN 'WorkingPixels' ELSE 'WorkingAlpha' END;
  expected_ref:=CASE position WHEN 0 THEN image->'pixels' ELSE image->'alpha' END;
  IF array_item->>'contract' IS DISTINCT FROM contract OR array_item->'ref' IS DISTINCT FROM expected_ref
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(expected_ref->>'artifact_id')::uuid AND a.sha256=expected_ref->>'sha256'
   AND a.bytes=(array_item->>'bytes')::bigint AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type IN('application/octet-stream','application/x-npy') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v2_draft_artifact_scope(p_tenant,project,source.asset_version_id,a.id,a.sha256,contract,p_actor,p_session,source.artifact_id,source.sha256) IS TRUE) THEN RETURN false;END IF;
  position:=position+1;
 END LOOP;
 entry:=jsonb_build_object('pin_jcs',p_raw,'pin_sha256',p_sha);
 IF r.v30_watermark_sources ? (scope->>'preset_id') THEN RETURN r.v30_watermark_sources->(scope->>'preset_id')=entry;END IF;
 UPDATE photo.runs SET v30_watermark_sources=v30_watermark_sources||jsonb_build_object(scope->>'preset_id',entry) WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
