-- +goose Up
ALTER TABLE photo.backend_documents ADD COLUMN v28_restoration_preview_params text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v28_restoration_preview_body CHECK(
 v28_restoration_preview_params IS NULL OR (purpose='task_params'
  AND octet_length(v28_restoration_preview_params)=bytes
  AND encode(sha256(convert_to(v28_restoration_preview_params,'UTF8')),'hex')=sha256));

-- An internal pin may be called only by the scheduler-only reserve/complete
-- wrappers. It repeats all live review/array/quota authority on every call.
-- +goose StatementBegin
CREATE FUNCTION photo.pin_v28_restoration_preview_document(p_tenant uuid,p_run uuid,
 p_side text,p_variant text,p_image text,p_params text,p_bucket text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; s record; d photo.backend_documents%ROWTYPE;
 image jsonb; params jsonb; image_id uuid; image_sha text; pixel_id uuid; pixel_sha text;
 owner uuid; aid uuid:=gen_random_uuid(); h bytea; sha text; profile_sha text;
 size bigint; edge integer; capacity numeric; used numeric; role text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL OR p_side IS NULL OR p_variant IS NULL
 OR p_side NOT IN('before','after') OR p_variant NOT IN('preview1600','preview2560')
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'preview' OR r.status<>'draft' OR r.graph_sealed
 OR r.cancel_requested OR r.v28_restoration_preview_task_id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_source(p_tenant,r.v28_restoration_preview_task_id);
 IF NOT FOUND OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM s.result_id::text
 OR r.config_snapshot->>'source_review_id' IS DISTINCT FROM s.review_id::text
 OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM s.attempt::text
 OR r.config_snapshot->>'source_fence' IS DISTINCT FROM s.fence::text
 OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
 OR r.config_snapshot->'before' IS DISTINCT FROM jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
 OR r.config_snapshot->'after' IS DISTINCT FROM jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
 OR r.config_snapshot->>'actor_id' IS DISTINCT FROM s.actor_id::text
 OR r.config_snapshot->>'auth_session_id' IS DISTINCT FROM s.auth_session_id::text
 OR coalesce(r.config_snapshot->>'srgb_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 image_id:=CASE p_side WHEN 'before' THEN s.source_id ELSE s.candidate_id END;
 image_sha:=CASE p_side WHEN 'before' THEN s.source_sha ELSE s.candidate_sha END;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v28_restoration_preview_task_id FOR UPDATE;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=s.review_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=image_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v28_restoration_preview_object(p_tenant,
  r.v28_restoration_preview_task_id,s.review_id,s.result_id,s.root_id,s.root_sha,
  p_side,image_id,image_sha) o WHERE o.bucket=p_bucket AND o.bytes=octet_length(p_image))
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex')<>image_sha THEN RETURN NULL;END IF;
 image:=p_image::jsonb;params:=p_params::jsonb;
 IF jsonb_typeof(image)<>'object' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR jsonb_typeof(params)<>'object'
 OR NOT params ?& ARRAY['image','variant','profile_hash','source_kind','suffix','matte_rgb']
 OR (params-'image'-'variant'-'profile_hash'-'source_kind'-'suffix'-'matte_rgb')<>'{}'::jsonb
 OR params->'image' IS DISTINCT FROM jsonb_build_object('artifact_id',image_id,'sha256',image_sha)
 OR params->>'variant' IS DISTINCT FROM p_variant
 OR params->>'source_kind' IS DISTINCT FROM 'working'
 OR params->'suffix' IS DISTINCT FROM 'null'::jsonb
 OR params->'matte_rgb' IS DISTINCT FROM 'null'::jsonb THEN RETURN NULL;END IF;
 edge:=CASE p_variant WHEN 'preview1600' THEN 1600 ELSE 2560 END;
 profile_sha:=encode(sha256(convert_to(format(
  '{"icc_sha256":"%s","long_edge":%s,"metadata_policy":"stripped","no_upscale":true,"quality":88,"schema_version":1,"variant":"%s"}',
  r.config_snapshot->>'srgb_sha256',edge,p_variant),'UTF8')),'hex');
 IF params->>'profile_hash' IS DISTINCT FROM profile_sha THEN RETURN NULL;END IF;
 IF jsonb_typeof(image->'pixels')<>'object' OR
  NOT image->'pixels' ?& ARRAY['artifact_id','sha256'] OR
  (image->'pixels'-'artifact_id'-'sha256')<>'{}'::jsonb THEN RETURN NULL;END IF;
 pixel_id:=(image->'pixels'->>'artifact_id')::uuid;
 pixel_sha:=image->'pixels'->>'sha256';
 role:=CASE p_side WHEN 'before' THEN 'source_pixels' ELSE 'output_pixels' END;
 IF photo.v28_restoration_result_array(p_tenant,r.v28_restoration_preview_task_id,
  s.result_id,s.root_id,s.root_sha,role,pixel_id,pixel_sha,p_bucket) IS NULL THEN RETURN NULL;END IF;
 IF image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb THEN
  IF jsonb_typeof(image->'alpha')<>'object' OR NOT image->'alpha' ?& ARRAY['artifact_id','sha256']
   OR (image->'alpha'-'artifact_id'-'sha256')<>'{}'::jsonb THEN RETURN NULL;END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(image->'alpha'->>'artifact_id')::uuid AND a.sha256=image->'alpha'->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v2_draft_artifact_scope(p_tenant,s.project_id,s.source_version_id,a.id,a.sha256,
    'WorkingAlpha',s.actor_id,s.auth_session_id,s.source_id,s.source_sha)) THEN RETURN NULL;END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant
  AND q.holder_type='run' AND q.holder_id=p_run AND q.state='reserved'
  AND q.expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(p_run)||convert_to('restoration.'||p_side||':'||p_variant,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 owner:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');size:=octet_length(p_params);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant
  AND run_id=p_run AND owner_id=owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.v28_restoration_preview_params=p_params AND d.sha256=sha AND d.bytes=size
   AND d.bucket=p_bucket THEN RETURN d.artifact_id;END IF;
  RETURN NULL;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN NULL;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,
  sha256,bytes,bucket,v28_restoration_preview_params)
 VALUES(p_tenant,p_run,owner,'task_params',aid,gen_random_uuid(),sha,size,p_bucket,p_params);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN aid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pin_v28_restoration_preview_document(uuid,uuid,text,text,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v28_restoration_preview_document(uuid,uuid,text,text,text,text,text)
 FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v28_restoration_preview_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_side text,p_variant text,p_sha text,p_size bigint,p_bucket text,
 p_image text,p_params text)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected uuid;h bytea;pinned uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 1048576
 OR p_size IS DISTINCT FROM octet_length(p_params)::bigint
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex')
 OR p_run IS NULL OR p_side IS NULL OR p_variant IS NULL
 OR p_side NOT IN('before','after') OR p_variant NOT IN('preview1600','preview2560') THEN RETURN;END IF;
 h:=public.digest(uuid_send(p_run)||convert_to('restoration.'||p_side||':'||p_variant,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_owner IS DISTINCT FROM expected THEN RETURN;END IF;
 pinned:=photo.pin_v28_restoration_preview_document(p_tenant,p_run,p_side,p_variant,p_image,p_params,p_bucket);
 IF pinned IS NULL THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.artifact_id=pinned AND b.purpose='task_params';
 IF NOT FOUND OR d.state NOT IN('pending','ready') THEN RETURN;END IF;
 IF d.state='ready' AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.output_role='task_params'
  AND a.media_type='application/json' AND a.verified_at IS NOT NULL
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN;END IF;
 RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v28_restoration_preview_document(uuid,uuid,uuid,text,text,text,bigint,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v28_restoration_preview_document(uuid,uuid,uuid,text,text,text,bigint,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.reserve_v28_restoration_preview_document(uuid,uuid,uuid,text,text,text,bigint,text,text,text)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v28_restoration_preview_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,
 p_version text,p_side text,p_variant text,p_image text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;pinned uuid;target text;
BEGIN
 IF p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024
 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant
  AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.v28_restoration_preview_params IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha
 OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 pinned:=photo.pin_v28_restoration_preview_document(p_tenant,p_run,p_side,p_variant,
  p_image,d.v28_restoration_preview_params,p_bucket);
 IF pinned IS DISTINCT FROM p_artifact THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.sha256=p_sha
  AND a.bytes=p_bytes AND a.bucket=p_bucket AND a.object_key=target AND a.object_version=p_version
  AND a.output_role='task_params' AND a.media_type='application/json'
  AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,
  media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,
  'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,
  storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'RESTORATION_PREVIEW_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v28_restoration_preview_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v28_restoration_preview_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.complete_v28_restoration_preview_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
