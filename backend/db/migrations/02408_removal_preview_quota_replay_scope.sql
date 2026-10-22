-- +goose Up
-- Full presentation bounds; immutable existing profiles are never upgraded in place.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_semantic_removal_preview_quota(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_source text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_id uuid; e jsonb; image jsonb; estimate jsonb; field text; pixels bigint;
 width bigint; height bigint; budget bigint; project uuid; usage photo.tenant_usage%ROWTYPE;
 old photo.quota_reservations%ROWTYPE; frozen jsonb; capacity numeric;
BEGIN
 run_id:=photo.lock_semantic_removal_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR encode(public.digest(convert_to(p_source,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'source'->'source'->>'sha256'
 THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(e->'source'->'source'->>'artifact_id')::uuid
 AND sha256=e->'source'->'source'->>'sha256' AND bytes=octet_length(p_source)
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 image:=p_source::jsonb;
 IF jsonb_typeof(image) IS DISTINCT FROM 'object' OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR image->'color'->>'encoding' IS DISTINCT FROM 'gamma24'
 OR image->'canvas'->>'width' IS NULL OR image->'canvas'->>'height' IS NULL
 OR image->'canvas'->>'width' !~ '^[1-9][0-9]{0,5}$'
 OR image->'canvas'->>'height' !~ '^[1-9][0-9]{0,5}$' THEN RETURN false;END IF;
 width:=(image->'canvas'->>'width')::bigint;height:=(image->'canvas'->>'height')::bigint;
 pixels:=width*height;
 IF width>200000 OR height>200000 OR pixels NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 estimate:=p_profile->'resource_estimate';
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object'
 OR NOT(p_profile ?& ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification','model_lock_id'])
 OR (p_profile-'backend'-'renderer_version'-'resource_estimate'-'model_lock_sha'-'runtime_lock_sha'-'device_profile_sha'-'dataset_sha'-'qualification'-'model_lock_id')<>'{}'::jsonb
 OR coalesce(p_profile->>'backend','') NOT IN('cpu','cuda','mps')
 OR p_profile->>'model_lock_id' IS DISTINCT FROM p_profile->>'model_lock_sha' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR jsonb_typeof(estimate) IS DISTINCT FROM 'object'
 OR NOT(estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate-'ram_bytes'-'vram_bytes'-'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'] LOOP
  IF jsonb_typeof(estimate->field) IS DISTINCT FROM 'number' OR estimate->>field !~ '^(0|[1-9][0-9]*)$'
  OR (estimate->>field)::numeric>9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 IF (estimate->>'pixel_count')::bigint<>pixels OR (p_profile->>'backend'='cpu' AND (estimate->>'vram_bytes')::bigint<>0)
 OR (p_profile->>'backend'<>'cpu' AND (estimate->>'vram_bytes')::bigint<1)
 OR (p_profile->>'backend'='cuda' AND (estimate->>'vram_bytes')::bigint>10000000000)
 OR (estimate->>'ram_bytes')::bigint<768*pixels+536870912+1
 OR (estimate->>'scratch_bytes')::bigint<32*pixels+4*16384+16777216+5*least(pixels,6553600)+1048576+65536
 OR (estimate->>'output_bytes')::bigint<12*pixels+16384+20*least(pixels,6553600)+4*65536+8*1048576 THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
  IF p_profile->>field IS DISTINCT FROM e->>field THEN RETURN false;END IF;
 END LOOP;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF NOT photo.v27_downstream_profile_ready(p_tenant,project,'semantic.preview_remove_object','1','gpu',p_profile) THEN RETURN false;END IF;
 SELECT v27_preapply_preview_profile INTO frozen FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id;
 IF frozen IS NOT NULL AND frozen IS DISTINCT FROM p_profile THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 budget:=(estimate->>'output_bytes')::bigint;
 SELECT * INTO old FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=run_id FOR UPDATE;
 IF FOUND THEN RETURN coalesce(frozen=p_profile,false) AND old.state='reserved' AND old.principal_type='user'
 AND old.principal_id=p_actor AND old.share_id IS NULL AND old.bytes=budget AND old.files=0 AND old.expires_at>clock_timestamp();END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 IF capacity IS NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>capacity
 OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',run_id,budget,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+budget WHERE tenant_id=p_tenant;
 UPDATE photo.runs SET v27_preapply_preview_profile=p_profile WHERE tenant_id=p_tenant AND id=run_id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.reserve_semantic_removal_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_semantic_removal_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.reserve_semantic_removal_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
