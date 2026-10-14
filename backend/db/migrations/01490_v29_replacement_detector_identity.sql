-- +goose Up
-- Private preliminary identity gate, not source/admission authority.
-- Caller retains locks and must separately prove mapped physical source/arrays.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_replacement_detector_identity(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;n jsonb;intent jsonb;original jsonb;params jsonb;image jsonb;profile jsonb;
 namespace uuid;replacement_run uuid;replacement_task uuid;detector_run uuid;seed text;pixels bigint;per_pixel bigint;
BEGIN
 IF p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 3145728
 OR p_original IS NULL OR octet_length(p_original) NOT BETWEEN 2 AND 65536
 OR photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 d:=p_descriptor::jsonb;n:=d->'replacement';
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(d) k) IS DISTINCT FROM ARRAY[
 'actor_id','auth_session_id','base','decision_hash','kind','params_jcs','profile_jcs','project_id','replacement','review_id','run_id','schema_version','source_jcs','task_id','tenant_id']::text[]
 OR d->'schema_version' IS DISTINCT FROM '1'::jsonb OR d->>'kind' IS DISTINCT FROM 'v29_perspective_replacement_detection_v1'
 OR d->>'tenant_id' IS DISTINCT FROM p_tenant::text OR d->>'project_id' IS DISTINCT FROM p_project::text
 OR d->>'actor_id' IS DISTINCT FROM p_actor::text OR d->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR d->>'review_id' IS DISTINCT FROM p_review::text OR d->>'decision_hash' IS DISTINCT FROM p_hash
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(n) k) IS DISTINCT FROM ARRAY['original_run_id','original_task_id','parents','run_id','task_id']::text[]
 OR jsonb_typeof(n->'parents') IS DISTINCT FROM 'array' OR jsonb_array_length(n->'parents')>4096 THEN RETURN false;END IF;
 intent:=photo.read_v29_negative_frozen_intent_locked(p_tenant,p_project,p_actor,p_session,p_review,p_hash,(n->>'original_task_id')::uuid);
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM 'optical.perspective'
 OR intent->>'original_run_id' IS DISTINCT FROM n->>'original_run_id'
 OR encode(sha256(convert_to(p_original,'UTF8')),'hex') IS DISTINCT FROM intent->'params'->>'sha256'
 OR octet_length(p_original) IS DISTINCT FROM (intent->'params'->>'bytes')::bigint THEN RETURN false;END IF;
 original:=p_original::jsonb;
 IF original->>'mode' IS DISTINCT FROM 'auto' OR original->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR original->'base'->>'asset_version_id' IS DISTINCT FROM d->'base'->>'asset_version_id' THEN RETURN false;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||p_review::text||':'||p_hash);
 replacement_task:=photo.v30_uuid5(namespace,'task:'||(n->>'original_task_id'));
 replacement_run:=photo.v30_uuid5(namespace,'run:'||(n->>'original_run_id')||':'||(n->>'original_task_id'));
 detector_run:=photo.v30_uuid5(replacement_run,'V29.PerspectiveReplacementDetection/v1');
 IF n->>'task_id' IS DISTINCT FROM replacement_task::text OR n->>'run_id' IS DISTINCT FROM replacement_run::text
 OR d->>'run_id' IS DISTINCT FROM detector_run::text THEN RETURN false;END IF;
 IF jsonb_typeof(d->'params_jcs') IS DISTINCT FROM 'string' OR octet_length(d->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(d->'source_jcs') IS DISTINCT FROM 'string' OR octet_length(d->>'source_jcs') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(d->'profile_jcs') IS DISTINCT FROM 'string' OR octet_length(d->>'profile_jcs') NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 params:=(d->>'params_jcs')::jsonb;image:=(d->>'source_jcs')::jsonb;profile:=(d->>'profile_jcs')::jsonb;
 IF params IS DISTINCT FROM jsonb_build_object('schema_version',1,'base',d->'base')
 OR encode(sha256(convert_to(d->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM d->'base'->'source'->>'sha256'
 OR image->>'dtype' IS DISTINCT FROM 'float32' OR image->'color'->>'encoding' IS DISTINCT FROM 'linear'
 OR coalesce(image->'color'->>'icc_sha256','') !~ '^[0-9a-f]{64}$'
 OR (image->'canvas'->>'width')::bigint NOT BETWEEN 2 AND 200000000
 OR (image->'canvas'->>'height')::bigint NOT BETWEEN 2 AND 200000000 THEN RETURN false;END IF;
 pixels:=(image->'canvas'->>'width')::bigint*(image->'canvas'->>'height')::bigint;
 IF pixels IS NULL OR pixels NOT BETWEEN 4 AND 200000000 THEN RETURN false;END IF;
 per_pixel:=CASE WHEN image->'alpha' IS NULL OR image->'alpha'='null'::jsonb THEN 12 ELSE 16 END;
 IF profile IS DISTINCT FROM jsonb_build_object('backend','cpu','precision','float32','renderer_version','v2.2',
 'resource_estimate',jsonb_build_object('ram_bytes',pixels*per_pixel+268435456,'scratch_bytes',pixels*per_pixel+33554432,
 'output_bytes',1048576,'vram_bytes',0,'pixel_count',pixels)) THEN RETURN false;END IF;
 -- Params bytes are already closed and must be canonical at Go restore; bind
 -- task seed to those exact bytes, not PostgreSQL's jsonb text whitespace.
 seed:='{"params":'||(d->>'params_jcs')||',"project":'||to_jsonb(p_project::text)::text||',"tenant":'||to_jsonb(p_tenant::text)::text||',"working_icc_sha":'||to_jsonb(image->'color'->>'icc_sha256')::text||'}';
 IF d->>'task_id' IS DISTINCT FROM photo.v30_uuid5(detector_run,'V29.PerspectiveLines:'||seed)::text THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_replacement_detector_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_replacement_detector_identity(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
