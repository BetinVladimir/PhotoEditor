-- +goose Up
-- Read retry pins under the same pending request and current source authority.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_watermark_pin_retry(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_preset uuid,p_preset_sha text,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,pin_jcs text,pin_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;entry jsonb;body jsonb;frozen jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_preset IS NULL OR p_preset_sha IS NULL OR p_preset_sha !~ '^[0-9a-f]{64}$'
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR p_run IS DISTINCT FROM photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key) THEN RETURN;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL THEN RETURN;END IF;
 SELECT (value->>'body_jcs')::jsonb INTO frozen FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_input';
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(frozen->'presets') WHERE value->>'preset_id'=p_preset::text AND value->>'preset_sha'=p_preset_sha)
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_preset' AND value->'ref'->>'sha256'=p_preset_sha
 AND jsonb_typeof(((value->>'body_jcs')::jsonb)->'watermark')='object') THEN RETURN;END IF;
 SELECT r.v30_watermark_sources->p_preset::text INTO entry FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.created_by=p_actor AND r.kind='v2' AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND w.project_id=p_project AND w.workflow_type='delivery' AND w.renderer_version='v2.2';
 IF NOT FOUND THEN RETURN;END IF;
 IF entry IS NULL THEN RETURN QUERY SELECT 'absent'::text,NULL::text,NULL::text;RETURN;END IF;
 body:=(entry->>'pin_jcs')::jsonb;
 IF body->'scope'->>'preset_id' IS DISTINCT FROM p_preset::text OR body->'scope'->>'preset_sha256' IS DISTINCT FROM p_preset_sha
 OR photo.pin_v30_watermark_source(p_tenant,p_run,p_actor,p_session,p_key,p_request_sha,entry->>'pin_jcs',entry->>'pin_sha256') IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT 'saved'::text,entry->>'pin_jcs',entry->>'pin_sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_watermark_pin_retry(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_watermark_pin_retry(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_watermark_pin_retry(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
