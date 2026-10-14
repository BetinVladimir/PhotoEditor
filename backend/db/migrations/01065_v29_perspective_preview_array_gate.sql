-- +goose Up
-- Private post-IO witness check. It does not itself activate work.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_arrays_current(p_tenant uuid,p_run uuid,
 p_root text,p_image text,p_original text,p_arrays jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;source record;root jsonb;image jsonb;item jsonb;expected jsonb;
 role text;path text;loc record;ordinal integer:=0;total integer;identity boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_root IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 1048576
  OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
  OR p_original IS NULL OR octet_length(p_original) NOT BETWEEN 2 AND 65536
  OR jsonb_typeof(p_arrays) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.v29_perspective_preview_task_id IS NULL OR r.cancel_requested
  OR r.status NOT IN('draft','running','completed') THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.v29_perspective_preview_source(p_tenant,r.v29_perspective_preview_task_id);
 IF NOT FOUND OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM source.result_id::text
  OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM source.attempt::text
  OR r.config_snapshot->>'source_fence' IS DISTINCT FROM source.fence::text
  OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM source.root_sha
  OR encode(sha256(convert_to(p_original,'UTF8')),'hex') IS DISTINCT FROM source.source_sha THEN RETURN false;END IF;
 root:=p_root::jsonb;image:=p_image::jsonb;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM root->'working_image'->>'sha256'
  OR image->>'dtype' IS DISTINCT FROM 'float32' THEN RETURN false;END IF;
 identity:=root->'working_image'=jsonb_build_object('artifact_id',source.source_id,'sha256',source.source_sha);
 total:=CASE WHEN image->'alpha'='null'::jsonb THEN 1 ELSE 2 END;
 IF jsonb_array_length(p_arrays)<>total THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_arrays) LOOP
  role:=CASE ordinal WHEN 0 THEN 'WorkingPixels' ELSE 'WorkingAlpha' END;
  path:=CASE WHEN identity THEN '$/source/' ELSE '$/working_image/' END||CASE ordinal WHEN 0 THEN 'pixels' ELSE 'alpha' END;
  expected:=CASE ordinal WHEN 0 THEN image->'pixels' ELSE image->'alpha' END;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT item ?& ARRAY['ref','parent','path','contract','object_key','object_version','sha256','bytes','media_type']
   OR (item-'ref'-'parent'-'path'-'contract'-'object_key'-'object_version'-'sha256'-'bytes'-'media_type')<>'{}'::jsonb
   OR item->'ref' IS DISTINCT FROM expected OR item->'parent' IS DISTINCT FROM root->'working_image'
   OR item->>'path' IS DISTINCT FROM path OR item->>'contract' IS DISTINCT FROM role
   OR item->>'sha256' IS DISTINCT FROM expected->>'sha256' THEN RETURN false;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(expected->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  IF identity THEN
   SELECT * INTO loc FROM photo.v29_perspective_preview_original_array(p_tenant,r.v29_perspective_preview_task_id,
    source.result_id,source.attempt,source.fence,p_original,(expected->>'artifact_id')::uuid,expected->>'sha256',role);
  ELSE
   SELECT * INTO loc FROM photo.v29_perspective_preview_object(p_tenant,r.v29_perspective_preview_task_id,
    source.result_id,source.attempt,source.fence,source.root_id,source.root_sha,
    (expected->>'artifact_id')::uuid,expected->>'sha256',path,role);
  END IF;
  IF NOT FOUND OR item->>'object_key' IS DISTINCT FROM loc.object_key
   OR item->>'object_version' IS DISTINCT FROM loc.object_version
   OR item->'bytes' IS DISTINCT FROM to_jsonb(loc.bytes)
   OR item->>'media_type' IS DISTINCT FROM loc.media_type THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_perspective_preview_arrays_current(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_arrays_current(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
