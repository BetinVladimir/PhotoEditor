-- +goose Up
-- Replacement Params become ready before their task-input closure. Decode
-- only the saved bounded draft/descriptor and refuse malformed historical data.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_gc_draft_match(p_saved text,p_run uuid,
 p_owner uuid,p_artifact uuid,p_sha text,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE packet jsonb; identity jsonb; descriptor jsonb; recovery jsonb; body text;
BEGIN
 IF p_saved IS NULL OR octet_length(p_saved) NOT BETWEEN 2 AND 41943040
 OR p_run IS NULL OR p_owner IS NULL OR p_artifact IS NULL
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 packet:=p_saved::jsonb;
 IF jsonb_typeof(packet) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF packet ? 'identity' THEN
  identity:=packet->'identity';
  IF identity->>'params_document_id' IS DISTINCT FROM p_artifact::text
  OR identity->>'params_sha' IS DISTINCT FROM p_sha
  OR identity->>'run_id' IS DISTINCT FROM p_run::text
  OR identity->>'task_id' IS DISTINCT FROM p_owner::text THEN RETURN false;END IF;
  IF jsonb_typeof(packet->'draft')='object' THEN
   descriptor:=packet->'draft';
   IF descriptor->>'run_id' IS DISTINCT FROM p_run::text
   OR descriptor->>'task_id' IS DISTINCT FROM p_owner::text
   OR descriptor->>'params_document_id' IS DISTINCT FROM p_artifact::text
   OR descriptor->>'params_sha' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
  ELSE
   IF packet->'owner'->>'run_id' IS DISTINCT FROM p_run::text
   OR packet->'owner'->>'task_id' IS DISTINCT FROM p_owner::text
   OR jsonb_typeof(packet->'recovery_jcs') IS DISTINCT FROM 'string'
   OR octet_length(packet->>'recovery_jcs') NOT BETWEEN 2 AND 2097152 THEN RETURN false;END IF;
   recovery:=(packet->>'recovery_jcs')::jsonb;
   IF jsonb_typeof(recovery) IS DISTINCT FROM 'object'
   OR jsonb_typeof(recovery->'descriptor_jcs') IS DISTINCT FROM 'string'
   OR octet_length(recovery->>'descriptor_jcs') NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
   descriptor:=(recovery->>'descriptor_jcs')::jsonb;
   IF descriptor->'params_ref' IS DISTINCT FROM
    jsonb_build_object('artifact_id',p_artifact::text,'sha256',p_sha) THEN RETURN false;END IF;
  END IF;
 ELSE
  descriptor:=packet;
  IF descriptor->>'run_id' IS DISTINCT FROM p_run::text
  OR descriptor->>'task_id' IS DISTINCT FROM p_owner::text
  OR photo.v30_uuid5(p_owner,'params') IS DISTINCT FROM p_artifact THEN RETURN false;END IF;
 END IF;
 IF jsonb_typeof(descriptor) IS DISTINCT FROM 'object'
 OR jsonb_typeof(descriptor->'params_jcs') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 body:=descriptor->>'params_jcs';
 RETURN octet_length(body)=p_bytes
  AND encode(sha256(convert_to(body,'UTF8')),'hex')=p_sha;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value
 OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_replacement_gc_draft_match(text,uuid,uuid,uuid,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_gc_draft_match(text,uuid,uuid,uuid,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_replacement_gc_draft_match(text,uuid,uuid,uuid,text,bigint) TO photo_fn_owner;

-- +goose StatementBegin
DO $$
DECLARE body text; updated text;
BEGIN
 SELECT prosrc INTO body FROM pg_proc
 WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure
 AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]'
    OR strpos(body,'v29_negative_gc_draft_match')=0
    OR strpos(body,'v29_replacement_gc_draft_match')>0 THEN
  RAISE EXCEPTION 'V29_REPLACEMENT_GC_SHAPE';
 END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',
  E'\\1 EXISTS(SELECT FROM photo.backend_documents d JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose=''task_params'' AND d.state=''ready'' AND r.kind=''v2'' AND r.status=''draft'' AND NOT r.cancel_requested AND (photo.v29_replacement_gc_draft_match(r.config_snapshot->>''v29_negative_replacement_draft_jcs'',r.id,d.owner_id,d.artifact_id,d.sha256,d.bytes) OR photo.v29_replacement_gc_draft_match(r.config_snapshot->>''v29_perspective_replacement_detection_jcs'',r.id,d.owner_id,d.artifact_id,d.sha256,d.bytes))) OR ','i');
 IF updated=body THEN RAISE EXCEPTION 'V29_REPLACEMENT_GC_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
