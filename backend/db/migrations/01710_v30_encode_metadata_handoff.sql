-- +goose Up
-- Physical verification service keeps one ABI; metadata delivery stops at a
-- barrier rather than adopting a stripped intermediate as the final export.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)
 RENAME TO adopt_v30_delivery_encode_strip;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_encode_strip(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE FUNCTION photo.adopt_v30_delivery_encode(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_source text,p_manifest text,p_raster_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE child uuid;mode text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT r.id,((r.config_snapshot->'v30_delivery_export'->>'config_jcs')::jsonb)->>'metadata_policy'
 INTO child,mode FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='export.encode' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result;
 IF child IS NULL OR mode IS NULL THEN RETURN false;END IF;
 IF mode='strip' THEN
 RETURN photo.adopt_v30_delivery_encode_strip(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_source,p_manifest,p_raster_bytes);
 ELSIF mode IN('copyright','approved_exif') THEN
 RETURN photo.begin_v30_metadata_barrier(p_tenant,child,p_attempt,p_fence,p_result,p_params,p_source,p_manifest,p_raster_bytes);
 END IF;
 RETURN false;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
