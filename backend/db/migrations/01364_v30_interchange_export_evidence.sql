-- +goose Up
-- Immutable pre-finalization evidence; saving it does not adopt/finalize a run.
-- +goose StatementBegin
CREATE TABLE photo.v30_interchange_export_evidence(
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,result_id uuid NOT NULL,
 root_id uuid NOT NULL,root_sha text NOT NULL CHECK(root_sha ~ '^[0-9a-f]{64}$'),
 request_sha text NOT NULL CHECK(request_sha ~ '^[0-9a-f]{64}$'),
 evidence_sha text NOT NULL CHECK(evidence_sha ~ '^[0-9a-f]{64}$'),
 evidence_jcs text NOT NULL CHECK(octet_length(evidence_jcs) BETWEEN 2 AND 16777216),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(tenant_id,run_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,result_id) REFERENCES photo.task_results(tenant_id,id),
 FOREIGN KEY(tenant_id,root_id) REFERENCES photo.artifacts(tenant_id,id)
);
ALTER TABLE photo.v30_interchange_export_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_interchange_export_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_interchange_export_evidence TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_interchange_export_evidence FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT ON photo.v30_interchange_export_evidence TO photo_fn_owner;
CREATE FUNCTION photo.save_v30_interchange_export_evidence(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;saved jsonb;frozen jsonb;rid uuid;wid uuid;tid uuid;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 existing photo.v30_interchange_export_evidence%ROWTYPE;ref jsonb;source jsonb;meta record;ordinal integer;contract_name text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 16777216 OR p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_digest
 OR p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(e))<>17
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb OR e->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR e->>'actor_id' IS DISTINCT FROM p_actor::text OR e->>'request_sha256' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Interchange.Export.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL THEN RETURN false;END IF;frozen:=saved->'frozen';
 -- Frozen SHA is checked against the backend's canonical payload on save;
 -- embedded strings/identity/request are checked here without reserializing JSONB.
 IF e->>'format' IS DISTINCT FROM frozen->'request'->>'format' OR e->>'frozen_sha256' IS NULL OR e->>'frozen_sha256' !~ '^[0-9a-f]{64}$' OR e->'identity' IS DISTINCT FROM frozen->'identity'
 OR e->>'manifest_sha256' IS NULL OR e->>'manifest_sha256' !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(e->'documents') IS DISTINCT FROM 'array' OR jsonb_array_length(e->'documents') NOT BETWEEN 3 AND 52
 OR jsonb_typeof(e->'files') IS DISTINCT FROM 'array' OR jsonb_array_length(e->'files') NOT BETWEEN 1 AND 30000
 OR jsonb_typeof(e->'sources') IS DISTINCT FROM 'array' OR jsonb_array_length(e->'sources')>30000 THEN RETURN false;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR t.canonical_result_id IS NULL THEN RETURN false;END IF;
 -- Existing output catalogue enforces task/canonical attempt/fence and current
 -- session/snapshot/project/download ACL; all receipts must be the same result.
 PERFORM 1 FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,(e->'root'->>'artifact_id')::uuid,e->'root'->>'sha256','InterchangeResult');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id FOR SHARE;
 IF e->'documents'->0 IS DISTINCT FROM e->'root' OR jsonb_array_length(z.outputs)<>jsonb_array_length(e->'documents')+1
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(e->'documents'))<>jsonb_array_length(e->'documents')
 OR e->'documents' @> jsonb_build_array(e->'bundle') THEN RETURN false;END IF;
 ordinal:=0;
 FOR ref IN SELECT value FROM jsonb_array_elements(e->'documents') LOOP
  contract_name:=CASE ordinal WHEN 0 THEN 'InterchangeResult' WHEN 1 THEN 'MappingReport' ELSE 'MappingShard' END;
  PERFORM 1 FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,(ref->>'artifact_id')::uuid,ref->>'sha256',contract_name);
  IF NOT FOUND THEN RETURN false;END IF;ordinal:=ordinal+1;
 END LOOP;
 SELECT * INTO meta FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,(e->'bundle'->>'artifact_id')::uuid,e->'bundle'->>'sha256','InterchangeArchive');
 IF NOT FOUND OR meta.bucket IS DISTINCT FROM e->>'bucket' OR meta.object_key IS DISTINCT FROM e->>'object_key'
 OR meta.object_version IS DISTINCT FROM e->>'object_version' OR meta.bytes IS DISTINCT FROM (e->>'archive_bytes')::bigint THEN RETURN false;END IF;
 IF frozen->'request'->>'format'='photoeditor_bundle' THEN
  IF jsonb_array_length(e->'sources')<1 THEN RETURN false;END IF;
 ELSE
  IF jsonb_array_length(e->'sources')<>0 THEN RETURN false;END IF;
 END IF;
 IF (SELECT count(DISTINCT value->'ref'->>'artifact_id') FROM jsonb_array_elements(e->'sources'))<>jsonb_array_length(e->'sources') THEN RETURN false;END IF;
 FOR source IN SELECT value FROM jsonb_array_elements(e->'sources') LOOP
  PERFORM 1 FROM photo.v30_interchange_export_result_input(p_tenant,p_actor,p_session,p_key,p_request_sha,(e->'root'->>'artifact_id')::uuid,e->'root'->>'sha256',(source->'ref'->>'artifact_id')::uuid,source->'ref'->>'sha256',source->>'contract');
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO existing FROM photo.v30_interchange_export_evidence WHERE tenant_id=p_tenant AND run_id=rid FOR UPDATE;
 IF existing.run_id IS NOT NULL THEN RETURN existing.result_id=z.id AND existing.root_id=(e->'root'->>'artifact_id')::uuid
 AND existing.root_sha=e->'root'->>'sha256' AND existing.request_sha=p_request_sha AND existing.evidence_sha=p_digest AND existing.evidence_jcs=p_body;END IF;
 INSERT INTO photo.v30_interchange_export_evidence(tenant_id,run_id,result_id,root_id,root_sha,request_sha,evidence_sha,evidence_jcs)
 VALUES(p_tenant,rid,z.id,(e->'root'->>'artifact_id')::uuid,e->'root'->>'sha256',p_request_sha,p_digest,p_body);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v30_interchange_export_evidence(uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v30_interchange_export_evidence(uuid,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.save_v30_interchange_export_evidence(uuid,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
