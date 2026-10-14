-- +goose Up
-- Lock-only identity gate; no draft/task/queue mutation or quality attestation.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_auto_perspective_replacement_identity(p_tenant uuid,p_draft text,p_segments jsonb,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;packet jsonb;identity jsonb;draft jsonb;params jsonb;frozen jsonb;segment record;
 namespace uuid;replacement_run uuid;replacement_task uuid;owner uuid;sha text;identity_jcs text;
BEGIN
 IF p_draft IS NULL OR octet_length(p_draft) NOT BETWEEN 2 AND 3145728
 OR jsonb_typeof(p_segments) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 packet:=p_draft::jsonb;identity:=packet->'identity';draft:=packet->'draft';
 d:=(p_segments->'detection'->>'descriptor_jcs')::jsonb;
 params:=(draft->>'params_jcs')::jsonb;frozen:=(p_segments->'detection'->>'original_jcs')::jsonb;
 sha:=encode(sha256(convert_to(p_segments->>'segments_jcs','UTF8')),'hex');
 owner:=photo.v30_uuid5((d->>'task_id')::uuid,'V29.ReplacementSegments/v1:'||(p_segments->>'result_id')||':'||(p_segments->'root'->>'artifact_id')||':'||(p_segments->'root'->>'sha256')||':'||sha);
 SELECT * INTO segment FROM photo.read_v29_replacement_segments_ready(p_tenant,(d->>'run_id')::uuid,owner,sha,octet_length(p_segments->>'segments_jcs'),p_bucket,p_segments);
 IF NOT FOUND THEN RETURN false;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||(d->>'project_id')||':'||(d->>'review_id')||':'||(d->>'decision_hash'));
 replacement_task:=photo.v30_uuid5(namespace,'task:'||(d->'replacement'->>'original_task_id'));
 replacement_run:=photo.v30_uuid5(namespace,'run:'||(d->'replacement'->>'original_run_id')||':'||(d->'replacement'->>'original_task_id'));
 IF packet->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(packet) k) IS DISTINCT FROM ARRAY['draft','identity','identity_sha','input_digest','marker_jcs','schema_version']::text[]
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(identity) k) IS DISTINCT FROM ARRAY['decision_hash','marker_sha','operation','original_run_id','original_task_id','params_document_id','params_sha','project_id','review_id','run_id','schema_version','task_id','tenant_id','workflow_id']::text[]
 OR coalesce(packet->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR coalesce(identity->>'marker_sha','') !~ '^[0-9a-f]{64}$'
 OR identity->>'marker_sha' IS DISTINCT FROM encode(sha256(convert_to(packet->>'marker_jcs','UTF8')),'hex')
 OR (packet->>'marker_jcs')::jsonb IS DISTINCT FROM jsonb_build_object('version','v29_negative_replacement_v2','project_id',d->>'project_id','review_id',d->>'review_id','decision_hash',d->>'decision_hash','original_run_id',d->'replacement'->>'original_run_id','original_task_id',d->'replacement'->>'original_task_id')
 OR identity->>'operation' IS DISTINCT FROM 'optical.perspective' OR identity->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR identity->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR identity->>'project_id' IS DISTINCT FROM d->>'project_id'
 OR identity->>'review_id' IS DISTINCT FROM d->>'review_id'
 OR identity->>'decision_hash' IS DISTINCT FROM d->>'decision_hash'
 OR identity->>'original_task_id' IS DISTINCT FROM d->'replacement'->>'original_task_id'
 OR identity->>'original_run_id' IS DISTINCT FROM d->'replacement'->>'original_run_id'
 OR identity->>'run_id' IS DISTINCT FROM replacement_run::text OR identity->>'task_id' IS DISTINCT FROM replacement_task::text
 OR identity->>'workflow_id' IS DISTINCT FROM photo.v30_uuid5(replacement_run,'workflow')::text
 OR identity->>'params_document_id' IS DISTINCT FROM photo.v30_uuid5(replacement_task,'params')::text
 OR draft->>'tenant_id' IS DISTINCT FROM p_tenant::text OR draft->>'project_id' IS DISTINCT FROM d->>'project_id'
 OR draft->>'actor_id' IS DISTINCT FROM d->>'actor_id' OR draft->>'auth_session_id' IS DISTINCT FROM d->>'auth_session_id'
 OR draft->>'snapshot_id' IS DISTINCT FROM p_segments->'detection'->>'snapshot_id'
 OR draft->>'task_id' IS DISTINCT FROM identity->>'task_id' OR draft->>'run_id' IS DISTINCT FROM identity->>'run_id'
 OR draft->>'workflow_id' IS DISTINCT FROM identity->>'workflow_id'
 OR draft->>'params_document_id' IS DISTINCT FROM identity->>'params_document_id'
 OR draft->>'params_sha' IS DISTINCT FROM identity->>'params_sha'
 OR identity->>'params_sha' IS DISTINCT FROM encode(sha256(convert_to(draft->>'params_jcs','UTF8')),'hex')
 OR params->>'mode' IS DISTINCT FROM 'auto' OR frozen->>'mode' IS DISTINCT FROM 'auto'
 OR params->'base' IS DISTINCT FROM d->'base'
 OR params->'lines' IS DISTINCT FROM jsonb_build_object('artifact_id',segment.artifact_id,'sha256',sha)
 OR params->'lines' IS NOT DISTINCT FROM frozen->'lines'
 OR (params-'base'-'lines') IS DISTINCT FROM (frozen-'base'-'lines')
 OR draft->>'lines_jcs' IS DISTINCT FROM p_segments->>'segments_jcs'
 OR draft->>'source_jcs' IS DISTINCT FROM d->>'source_jcs' THEN RETURN false;END IF;
 SELECT '{'||string_agg(to_jsonb(key)::text||':'||value::text,',' ORDER BY key COLLATE "C")||'}' INTO identity_jcs FROM jsonb_each(identity);
 RETURN packet->>'identity_sha'=encode(sha256(convert_to(identity_jcs,'UTF8')),'hex');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_auto_perspective_replacement_identity(uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_auto_perspective_replacement_identity(uuid,text,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.lock_v29_auto_perspective_replacement_identity(uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
