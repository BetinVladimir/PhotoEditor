-- +goose Up
-- Persist physical verification under current SQL authority. This is deliberately
-- not v2-adopt and cannot mark an export/item ready or finalize a child run.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v30_delivery_encode_verification(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_source text,p_manifest text,p_raster_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;params jsonb;source jsonb;manifest jsonb;
 source_id uuid;binary_id uuid;marker uuid;parent_id uuid;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR p_manifest IS NULL OR octet_length(p_manifest) NOT BETWEEN 2 AND 1048576
 OR p_raster_bytes IS NULL OR p_raster_bytes<1 THEN RETURN false;END IF;
 params:=p_params::jsonb;source:=p_source::jsonb;manifest:=p_manifest::jsonb;
 source_id:=(params->'image'->>'artifact_id')::uuid;binary_id:=(manifest->'artifact'->>'artifact_id')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF r.id IS NULL THEN RETURN false;END IF;
 parent_id:=(r.config_snapshot->'v30_delivery_export'->>'parent_run_id')::uuid;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(r.id,parent_id) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO c FROM photo.v30_delivery_encode_context(p_tenant,p_task);
 IF NOT FOUND OR c.attempt IS DISTINCT FROM p_attempt OR c.fence IS DISTINCT FROM p_fence
 OR c.result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(c.params_artifact_id,c.root_artifact_id,source_id,binary_id)
 ORDER BY id FOR SHARE;
 PERFORM id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task AND attempt=p_attempt AND fence=p_fence
 ORDER BY id FOR SHARE;
 IF c.params_sha256 IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex')
 OR c.root_sha256 IS DISTINCT FROM encode(sha256(convert_to(p_manifest,'UTF8')),'hex') THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,c.params_artifact_id) a
 WHERE a.source='input' AND a.bytes=octet_length(p_params) AND a.sha256=c.params_sha256)
 OR NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,c.root_artifact_id) a
 WHERE a.source='output' AND a.role='encoded_manifest' AND a.bytes=octet_length(p_manifest) AND a.sha256=c.root_sha256)
 OR NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,source_id) a
 WHERE a.source='input' AND a.media_type='application/json' AND a.bytes=octet_length(p_source)
 AND a.sha256=params->'image'->>'sha256' AND a.sha256=encode(sha256(convert_to(p_source,'UTF8')),'hex'))
 OR NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_artifact(p_tenant,p_task,p_attempt,p_fence,p_result,binary_id) a
 WHERE a.source='output' AND a.role='encoded_image' AND a.bytes=p_raster_bytes
 AND a.sha256=manifest->'artifact'->>'sha256'
 AND a.media_type=CASE params->>'format' WHEN 'jpeg' THEN 'image/jpeg' WHEN 'png' THEN 'image/png' WHEN 'tiff' THEN 'image/tiff' ELSE '' END)
 THEN RETURN false;END IF;
 -- Delivery presentation has already executed size/sharpen in dedicated stages.
 IF params->>'size' IS NOT NULL OR params->>'metadata_policy' IS DISTINCT FROM 'strip'
 OR manifest->>'metadata_policy' IS DISTINCT FROM 'strip'
 OR manifest->>'format' IS DISTINCT FROM params->>'format'
 OR manifest->>'icc_id' IS DISTINCT FROM params->>'icc_id'
 OR manifest->>'icc_sha256' IS DISTINCT FROM params->>'icc_sha256'
 OR manifest->>'width' IS DISTINCT FROM source->'canvas'->>'width'
 OR manifest->>'height' IS DISTINCT FROM source->'canvas'->>'height'
 OR (manifest->>'alpha')::boolean IS DISTINCT FROM (source->>'alpha' IS NOT NULL AND params->>'alpha'='preserve')
 THEN RETURN false;END IF;
 -- Recheck after acquiring all producer/object locks. Receipt is tied to the
 -- immutable result ID; the final adopter must still repeat authority checks.
 IF NOT EXISTS(SELECT 1 FROM photo.v30_delivery_encode_context(p_tenant,p_task) x
 WHERE x.attempt=p_attempt AND x.fence=p_fence AND x.result_id=p_result
 AND x.root_artifact_id=c.root_artifact_id AND x.params_artifact_id=c.params_artifact_id) THEN RETURN false;END IF;
 marker:=photo.v2_adoption_event_id(p_task,p_result);
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
 VALUES(p_tenant,'v30-encode-verified',marker,clock_timestamp()) ON CONFLICT DO NOTHING;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v30_delivery_encode_verification(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v30_delivery_encode_verification(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v30_delivery_encode_verification(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
