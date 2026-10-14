-- +goose Up
ALTER TABLE photo.graph_expansions ADD COLUMN v30_metadata_proof_jcs text;
ALTER TABLE photo.graph_expansions ADD COLUMN v30_metadata_proof_sha text;
ALTER TABLE photo.graph_expansions ADD CONSTRAINT v30_metadata_proof_pair CHECK(
 (v30_metadata_proof_jcs IS NULL AND v30_metadata_proof_sha IS NULL) OR
 (v30_metadata_proof_jcs IS NOT NULL AND v30_metadata_proof_sha IS NOT NULL
 AND kind='v30.delivery.metadata/1' AND octet_length(v30_metadata_proof_jcs) BETWEEN 2 AND 8388608
 AND v30_metadata_proof_sha ~ '^[0-9a-f]{64}$'
 AND v30_metadata_proof_sha=encode(sha256(convert_to(v30_metadata_proof_jcs,'UTF8')),'hex')));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_metadata_proof() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.v30_metadata_proof_jcs IS NOT NULL AND
 (NEW.v30_metadata_proof_jcs IS DISTINCT FROM OLD.v30_metadata_proof_jcs OR NEW.v30_metadata_proof_sha IS DISTINCT FROM OLD.v30_metadata_proof_sha) THEN
  RAISE EXCEPTION 'IMMUTABLE_DELIVERY_METADATA_PROOF' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_v30_metadata_proof BEFORE UPDATE OF v30_metadata_proof_jcs,v30_metadata_proof_sha ON photo.graph_expansions
FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_metadata_proof();
ALTER FUNCTION photo.guard_v30_metadata_proof() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_metadata_proof() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.pin_v30_delivery_metadata_proof(p_tenant uuid,p_expansion uuid,p_token uuid,p_body text,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 proof jsonb;request jsonb;cfg jsonb;task_id uuid;approved text[];ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608 OR p_sha IS NULL
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') THEN RETURN false;END IF;
 proof:=p_body::jsonb;
 IF jsonb_typeof(proof) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(proof))<>15
 OR proof->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.state<>'open' OR e.expected_count<>1
 OR e.expansion_token IS DISTINCT FROM p_token OR p_token IS NULL OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion AND run_id=r.id FOR UPDATE;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.expected_count<>1 OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';cfg:=(request->>'config_jcs')::jsonb;
 IF proof->'binding' IS DISTINCT FROM request->'binding' OR proof->>'policy' IS DISTINCT FROM cfg->>'metadata_policy'
 OR proof->>'policy' NOT IN('copyright','approved_exif') THEN RETURN false;END IF;
 -- This field must be frozen by authenticated child admission, never by proof.
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
   FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved) THEN RETURN false;END IF;
 task_id:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0');
 IF proof->>'task_id' IS DISTINCT FROM task_id::text OR photo.v30_metadata_write_plan_valid(task_id,proof->'encoded',proof->>'encoded_jcs',
 proof->>'frozen_jcs',proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',proof->>'policy',approved,
 proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF proof->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(task_id,'delivery:metadata:params:1'),
 'sha256',encode(sha256(convert_to(proof->>'params_jcs','UTF8')),'hex'))
 OR proof->'projection' IS DISTINCT FROM jsonb_build_object('artifact_id',photo.v30_uuid5(task_id,'delivery:metadata:projection:1'),
 'sha256',encode(sha256(convert_to(proof->>'projection_jcs','UTF8')),'hex')) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=e.parent_task_id FOR SHARE;
 IF NOT FOUND OR t.operation<>'export.encode' OR t.operation_version<>'1' OR t.state<>'succeeded'
 OR t.cancel_requested OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.outputs @> jsonb_build_array(proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact')) THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact',proof->'metadata_source'] LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid
   AND a.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
   AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF e.v30_metadata_proof_jcs IS NOT NULL THEN RETURN e.v30_metadata_proof_jcs=p_body AND e.v30_metadata_proof_sha=p_sha;END IF;
 UPDATE photo.graph_expansions SET v30_metadata_proof_jcs=p_body,v30_metadata_proof_sha=p_sha WHERE id=e.id;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.pin_v30_delivery_metadata_proof(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v30_delivery_metadata_proof(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.pin_v30_delivery_metadata_proof(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
