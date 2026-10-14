-- +goose Up
-- Private current source authority reusable after the pending create ledger expires.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_pinned_source_ready(p_tenant uuid,p_parent uuid,p_actor uuid,p_session uuid,p_preset uuid,p_preset_sha text,p_config text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;project uuid;entry jsonb;body jsonb;scope jsonb;cfg jsonb;source record;image jsonb;item jsonb;expected jsonb;n integer:=0;contract text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 1048576
 OR p_preset_sha IS DISTINCT FROM encode(sha256(convert_to(p_config,'UTF8')),'hex') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent;
 IF NOT FOUND OR r.created_by IS DISTINCT FROM p_actor OR r.kind<>'v2' OR r.status NOT IN('draft','running') OR r.cancel_requested THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_parent AND w.workflow_type='delivery' AND w.renderer_version='v2.2';
 IF project IS NULL OR photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 entry:=r.v30_watermark_sources->p_preset::text;
 IF entry IS NULL OR octet_length(entry->>'pin_jcs') NOT BETWEEN 2 AND 2097152
 OR entry->>'pin_sha256' IS DISTINCT FROM encode(sha256(convert_to(entry->>'pin_jcs','UTF8')),'hex') THEN RETURN false;END IF;
 body:=(entry->>'pin_jcs')::jsonb;scope:=body->'scope';cfg:=p_config::jsonb;
 IF scope IS DISTINCT FROM jsonb_build_object('tenant_id',p_tenant,'project_id',project,'actor_id',p_actor,'parent_run_id',p_parent,'preset_id',p_preset,'preset_sha256',p_preset_sha)
 OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR cfg->'watermark'->>'asset_id' IS DISTINCT FROM body->>'asset_id'
 OR photo.v30_watermark_pin_geometry(body) IS DISTINCT FROM true
 OR r.v30_watermark_selections->p_preset::text IS DISTINCT FROM jsonb_build_object('asset_id',body->'asset_id','asset_version_id',body->'binding'->'asset_version_id','artifact_id',body->'binding'->'source'->'artifact_id','sha256',body->'binding'->'source'->'sha256','preset_sha256',scope->'preset_sha256') THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.v30_delivery_watermark_source(p_tenant,project,(body->>'asset_id')::uuid,(body->'binding'->>'asset_version_id')::uuid,p_actor,p_session);
 IF NOT FOUND OR body->'binding'->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',source.artifact_id,'sha256',source.sha256)
 OR source.sha256 IS DISTINCT FROM encode(sha256(convert_to(body->>'manifest_jcs','UTF8')),'hex') THEN RETURN false;END IF;
 image:=(body->>'manifest_jcs')::jsonb;
 IF image->>'dtype' IS DISTINCT FROM 'float32' OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR image->'color'->>'encoding' IS DISTINCT FROM 'gamma24' THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(body->'arrays') LOOP
  contract:=CASE n WHEN 0 THEN 'WorkingPixels' ELSE 'WorkingAlpha' END;
  expected:=CASE n WHEN 0 THEN image->'pixels' ELSE image->'alpha' END;
  IF item->>'contract' IS DISTINCT FROM contract OR item->'ref' IS DISTINCT FROM expected
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(expected->>'artifact_id')::uuid AND a.sha256=expected->>'sha256'
   AND a.bytes=(item->>'bytes')::bigint AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.media_type IN('application/octet-stream','application/x-npy')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v2_draft_artifact_scope(p_tenant,project,source.asset_version_id,a.id,a.sha256,contract,p_actor,p_session,source.artifact_id,source.sha256) IS TRUE) THEN RETURN false;END IF;
  n:=n+1;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_pinned_source_ready(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_pinned_source_ready(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
