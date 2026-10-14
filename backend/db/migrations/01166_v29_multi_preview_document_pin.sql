-- +goose Up
-- Persist exact authored params before object IO using existing tenant-owned document lifecycle.
ALTER TABLE photo.backend_documents ADD COLUMN v29_multi_preview_params text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v29_multi_preview_body CHECK(
 v29_multi_preview_params IS NULL OR (purpose='task_params' AND octet_length(v29_multi_preview_params)=bytes
 AND encode(sha256(convert_to(v29_multi_preview_params,'UTF8')),'hex')=sha256));
-- +goose StatementBegin
CREATE FUNCTION photo.pin_v29_multi_preview_document(p_tenant uuid,p_run uuid,p_variant text,p_root text,p_image text,p_params text,p_bucket text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; source record; root jsonb; image jsonb; params jsonb;
 d photo.backend_documents%ROWTYPE; ref jsonb; role text; path text; owner uuid; h bytea; sha text; profile_sha text; edge integer;
 aid uuid:=gen_random_uuid(); size bigint; capacity numeric; used numeric;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 IF p_variant IS NULL OR p_variant NOT IN('preview1600','preview2560') OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR p_root IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 1048576
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_multi_preview_task_id IS NULL OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested THEN RETURN NULL;END IF;
 SELECT * INTO source FROM photo.v29_multi_preview_intent_source(p_tenant,p_run);
 IF NOT FOUND OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM source.result_id::text
 OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha)
 OR encode(sha256(convert_to(p_root,'UTF8')),'hex')<>source.root_sha THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v29_multi_preview_task_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_multi_preview_intent_source(p_tenant,p_run) s
  WHERE s.result_id=source.result_id AND s.root_id=source.root_id AND s.root_sha=source.root_sha AND s.attempt=source.attempt AND s.fence=source.fence) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=source.root_id AND a.bytes=octet_length(p_root)) THEN RETURN NULL;END IF;
 root:=p_root::jsonb;image:=p_image::jsonb;params:=p_params::jsonb;
 IF jsonb_typeof(root)<>'object' OR NOT root ?& ARRAY['schema_version','working_image','changed_mask','generated_mask','report']
 OR (root-'schema_version'-'working_image'-'changed_mask'-'generated_mask'-'report')<>'{}'::jsonb OR root->'schema_version' IS DISTINCT FROM '1'::jsonb OR root->'generated_mask' IS DISTINCT FROM 'null'::jsonb
 OR jsonb_typeof(image)<>'object' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'alpha' IS NULL OR image->'alpha'='null'::jsonb
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM root->'working_image'->>'sha256'
 OR jsonb_typeof(params)<>'object' OR NOT params ?& ARRAY['image','variant','profile_hash','source_kind','suffix','matte_rgb']
 OR (params-'image'-'variant'-'profile_hash'-'source_kind'-'suffix'-'matte_rgb')<>'{}'::jsonb
 OR params->'image' IS DISTINCT FROM root->'working_image' OR params->>'variant' IS DISTINCT FROM p_variant
 OR params->>'source_kind' IS DISTINCT FROM 'working' OR params->'suffix' IS DISTINCT FROM 'null'::jsonb OR params->'matte_rgb' IS DISTINCT FROM 'null'::jsonb THEN RETURN NULL;END IF;
 edge:=CASE p_variant WHEN 'preview1600' THEN 1600 ELSE 2560 END;
 profile_sha:=encode(sha256(convert_to(format('{"icc_sha256":"%s","long_edge":%s,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"%s"}',r.config_snapshot->>'srgb_sha256',edge,p_variant),'UTF8')),'hex');
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id::text=root->'working_image'->>'artifact_id' AND a.sha256=root->'working_image'->>'sha256' AND a.bytes=octet_length(p_image) AND a.media_type='application/json') THEN RETURN NULL;END IF;
 IF params->>'profile_hash' IS DISTINCT FROM profile_sha THEN RETURN NULL;END IF;
 FOR ref,role,path IN SELECT item.ref,item.role,item.path FROM (VALUES
  (root->'working_image','WorkingImage'::text,'$/working_image'::text),
  (image->'pixels','WorkingPixels'::text,'$/working_image/pixels'::text),
  (image->'alpha','WorkingAlpha'::text,'$/working_image/alpha'::text)) AS item(ref,role,path)
  WHERE item.ref IS NOT NULL AND item.ref<>'null'::jsonb LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT ref ?& ARRAY['artifact_id','sha256']
   OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb THEN RETURN NULL;END IF;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid FOR SHARE;
  IF NOT EXISTS(SELECT 1 FROM photo.v29_adopted_multi_child_object(p_tenant,source.project_id,r.v29_multi_preview_task_id,
    source.actor_id,source.auth_session_id,r.config_snapshot->>'operation',source.root_id,source.root_sha,
    (ref->>'artifact_id')::uuid,ref->>'sha256',path,role) a
    WHERE a.bucket=p_bucket AND a.object_version NOT IN('','null')) THEN RETURN NULL;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run AND q.state='reserved' AND q.bytes=111149056 AND q.files=0 AND q.principal_type='user' AND q.principal_id=r.created_by AND q.expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(p_run)||convert_to('render.derivative:'||p_variant,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);owner:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');size:=octet_length(p_params);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.v29_multi_preview_params=p_params AND d.sha256=sha AND d.bytes=size AND d.bucket=p_bucket THEN RETURN d.artifact_id;END IF;
  RETURN NULL;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN NULL;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v29_multi_preview_params)
 VALUES(p_tenant,p_run,owner,'task_params',aid,gen_random_uuid(),sha,size,p_bucket,p_params);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN aid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pin_v29_multi_preview_document(uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v29_multi_preview_document(uuid,uuid,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION photo.pin_v29_multi_preview_document(uuid,uuid,text,text,text,text,text) FROM photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
