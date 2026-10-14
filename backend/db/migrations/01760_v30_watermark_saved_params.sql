-- +goose Up
-- Current-pin recovery evidence only; callers must still check activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_saved_continuation_params(p_expansion uuid,p_token uuid,p_slot integer,p_operation text,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;e photo.graph_expansions%ROWTYPE;owner uuid;d photo.backend_documents%ROWTYPE;raw text;pin text;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('derivative.sharpen','derivative.watermark','export.encode') OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion
 AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid;
 IF NOT FOUND OR jsonb_typeof((request->>'config_jcs')::jsonb->'watermark') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 SELECT pin_jcs,pin_sha INTO raw,pin FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,
 (request->'preset'->>'preset_id')::uuid,request->'preset'->>'preset_sha');
 IF raw IS NULL OR pin IS NULL THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5((request->>'child_run_id')::uuid,'workflow'),p_operation||':0');
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=owner
 AND purpose='task_params' AND sha256=p_sha AND state='ready';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256);
END $$;
ALTER FUNCTION photo.v30_watermark_saved_continuation_params(uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_saved_continuation_params(uuid,uuid,integer,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_saved_continuation_params(uuid,uuid,integer,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
