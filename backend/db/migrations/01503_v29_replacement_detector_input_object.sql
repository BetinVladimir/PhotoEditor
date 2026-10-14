-- +goose Up
-- Metadata reader only. Go must retain complete mapped-source physical locks.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_detector_input_object(p_tenant uuid,p_run uuid,p_owner uuid,p_role text,p_artifact uuid,p_sha text,p_proof jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE descriptor jsonb;image jsonb;expected jsonb;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 descriptor:=(p_proof->>'descriptor_jcs')::jsonb;
 IF photo.v29_replacement_detector_params_document_scope(p_tenant,p_run,p_owner,'task_params',
 encode(sha256(convert_to(descriptor->>'params_jcs','UTF8')),'hex'),octet_length(descriptor->>'params_jcs'),p_proof) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 image:=(descriptor->>'source_jcs')::jsonb;
 expected:=CASE p_role WHEN 'WorkingImage' THEN descriptor->'base'->'source'
 WHEN 'WorkingPixels' THEN image->'pixels' WHEN 'WorkingAlpha' THEN image->'alpha' ELSE NULL END;
 IF expected IS NULL OR expected IS DISTINCT FROM jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha) THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR (p_role='WorkingImage' AND (a.media_type<>'application/json' OR a.bytes<>octet_length(descriptor->>'source_jcs')))
 OR (p_role<>'WorkingImage' AND (a.media_type NOT IN('application/x-npy','application/octet-stream') OR a.bytes NOT BETWEEN 1 AND 2400065536)) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_replacement_detector_input_object(uuid,uuid,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_detector_input_object(uuid,uuid,uuid,text,uuid,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_replacement_detector_input_object(uuid,uuid,uuid,text,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
