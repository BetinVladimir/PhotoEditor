-- Trusted qualification import. Policies and operators are provisioned only
-- by database administration; service roles cannot create either allowlist.
-- +goose Up
CREATE TABLE photo.v2_qualification_operators (
 operator_id uuid PRIMARY KEY REFERENCES photo.users(id),
 enabled boolean NOT NULL DEFAULT false
);
ALTER TABLE photo.v2_qualification_operators ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v2_qualification_operators FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v2_qualification_operators
 TO photo_fn_owner USING (true);
REVOKE ALL ON photo.v2_qualification_operators FROM PUBLIC,photo_api,
 photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,
 photo_maintenance,photo_gateway;
GRANT SELECT ON photo.v2_qualification_operators TO photo_fn_owner;

CREATE TABLE photo.v2_qualification_policies (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 operation text NOT NULL CHECK (length(operation) BETWEEN 1 AND 80),
 operation_version text NOT NULL CHECK (length(operation_version) BETWEEN 1 AND 20),
 model_lock_sha text NOT NULL CHECK (model_lock_sha ~ '^[0-9a-f]{64}$'),
 runtime_lock_sha text NOT NULL CHECK (runtime_lock_sha ~ '^[0-9a-f]{64}$'),
 backend text NOT NULL CHECK (backend IN ('cpu','cuda','mps')),
 device_profile_sha text NOT NULL CHECK (device_profile_sha ~ '^[0-9a-f]{64}$'),
 dataset_sha text NOT NULL CHECK (dataset_sha ~ '^[0-9a-f]{64}$'),
 issuer_id uuid NOT NULL REFERENCES photo.users(id),
 required_checks text[] NOT NULL CHECK (cardinality(required_checks)>0),
 test_fixture_shas text[] NOT NULL CHECK (cardinality(test_fixture_shas)>0),
 calibration_shas text[] NOT NULL DEFAULT '{}',
 evidence_shas text[] NOT NULL CHECK (cardinality(evidence_shas)>0),
 license_cleared boolean NOT NULL DEFAULT false,
 enabled boolean NOT NULL DEFAULT false,
 UNIQUE(operation,operation_version,model_lock_sha,runtime_lock_sha,
        device_profile_sha,dataset_sha,issuer_id)
);
ALTER TABLE photo.v2_qualification_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v2_qualification_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v2_qualification_policies
 TO photo_fn_owner USING (true);
REVOKE ALL ON photo.v2_qualification_policies FROM PUBLIC,photo_api,
 photo_worker,photo_scheduler,photo_auth,photo_portal,photo_publisher,
 photo_maintenance,photo_gateway;
GRANT SELECT ON photo.v2_qualification_policies TO photo_fn_owner;

ALTER TABLE photo.model_qualifications
 ADD COLUMN runtime_lock_sha text CHECK (runtime_lock_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN dataset_sha text CHECK (dataset_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN report_sha text CHECK (report_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN trusted_import boolean NOT NULL DEFAULT false;
CREATE INDEX model_qualifications_trusted_exact ON photo.model_qualifications
 (tenant_id,project_id,operation,operation_version,model_lock_sha,
  runtime_lock_sha,backend,device_profile_sha)
 WHERE trusted_import AND status='passed';
CREATE UNIQUE INDEX model_qualifications_trusted_report
 ON photo.model_qualifications(tenant_id,report_artifact_id)
 WHERE trusted_import;

-- +goose StatementBegin
CREATE FUNCTION photo.v2_qualification_report_object(
 p_tenant uuid,p_report uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes
 FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_report AND a.sha256=p_sha
  AND a.state='ready' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 8388608
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_qualification_report_object(uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_qualification_report_object(uuid,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_qualification_report_object(uuid,uuid,text)
 TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.import_v2_qualification(
 p_tenant uuid,p_project uuid,p_report uuid,p_sha text,p_operator uuid,
 p_backend text,p_body_text text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy_row photo.v2_qualification_policies%ROWTYPE;
 result_id uuid; event_id uuid; item jsonb; required text; p_body jsonb;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_report IS NULL OR
  p_operator IS NULL OR p_body_text IS NULL OR p_sha IS NULL OR
  p_backend IS NULL OR
  p_backend NOT IN ('cpu','cuda','mps') OR
  p_sha !~ '^[0-9a-f]{64}$' OR
  NOT EXISTS(SELECT 1 FROM photo.v2_qualification_operators o
   WHERE o.operator_id=p_operator AND o.enabled) OR
  NOT EXISTS(SELECT 1 FROM photo.memberships m
   WHERE m.tenant_id=p_tenant AND m.user_id=p_operator
   AND m.status='active' AND m.role IN ('owner','admin')) OR
  NOT EXISTS(SELECT 1 FROM photo.projects p
   WHERE p.tenant_id=p_tenant AND p.id=p_project) THEN
  RAISE EXCEPTION 'QUALIFICATION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF octet_length(p_body_text)>8388608 OR
  encode(public.digest(convert_to(p_body_text,'UTF8'),'sha256'),'hex')<>p_sha THEN
  RAISE EXCEPTION 'QUALIFICATION_REPORT_HASH' USING ERRCODE='23514';
 END IF;
 p_body:=p_body_text::jsonb;
 IF jsonb_typeof(p_body)<>'object' OR
  (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_body) key) <>
  ARRAY['checks','created_at','dataset_sha','device_profile_sha',
        'input_fixture_shas','issuer_id','metrics','model_lock_sha',
        'operation','runtime_lock_sha','schema_version','status','version'] OR
  p_body->>'schema_version'<>'1' OR p_body->>'status'<>'passed' OR
  jsonb_typeof(p_body->'checks')<>'array' OR
  jsonb_typeof(p_body->'input_fixture_shas')<>'array' OR
  jsonb_typeof(p_body->'metrics')<>'object' THEN
  RAISE EXCEPTION 'QUALIFICATION_REPORT_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO policy_row FROM photo.v2_qualification_policies p
  WHERE p.operation=p_body->>'operation'
   AND p.operation_version=p_body->>'version'
   AND p.model_lock_sha=p_body->>'model_lock_sha'
   AND p.runtime_lock_sha=p_body->>'runtime_lock_sha'
   AND p.backend=p_backend
   AND p.device_profile_sha=p_body->>'device_profile_sha'
   AND p.dataset_sha=p_body->>'dataset_sha'
   AND p.issuer_id=(p_body->>'issuer_id')::uuid
   AND p.enabled AND p.license_cleared;
 IF NOT FOUND OR jsonb_array_length(p_body->'checks')<1 OR
  jsonb_array_length(p_body->'input_fixture_shas')<1 OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_report AND a.sha256=p_sha
    AND a.state='ready' AND a.media_type='application/json'
    AND a.bytes=octet_length(p_body_text)
    AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'QUALIFICATION_REPORT_INVALID' USING ERRCODE='23514';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_body->'input_fixture_shas')
 LOOP
  IF jsonb_typeof(item)<>'string' OR item#>>'{}' <> ALL(policy_row.test_fixture_shas)
   OR item#>>'{}'=ANY(policy_row.calibration_shas) THEN
   RAISE EXCEPTION 'QUALIFICATION_FIXTURE_INVALID' USING ERRCODE='23514';
  END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(p_body->'checks') LOOP
  IF jsonb_typeof(item)<>'object' OR item->>'passed'<>'true' OR
   item->>'evidence_sha' <> ALL(policy_row.evidence_shas) THEN
   RAISE EXCEPTION 'QUALIFICATION_CHECK_INVALID' USING ERRCODE='23514';
  END IF;
 END LOOP;
 FOREACH required IN ARRAY policy_row.required_checks LOOP
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_body->'checks') c
   WHERE c->>'id'=required AND c->>'passed'='true') THEN
   RAISE EXCEPTION 'QUALIFICATION_CHECK_MISSING' USING ERRCODE='23514';
  END IF;
 END LOOP;
 PERFORM pg_catalog.pg_advisory_xact_lock(
  pg_catalog.hashtextextended(p_tenant::text||':'||p_report::text,0));
 SELECT q.id INTO result_id FROM photo.model_qualifications q
  WHERE q.tenant_id=p_tenant AND q.project_id=p_project
   AND q.report_artifact_id=p_report;
 IF FOUND THEN
  IF NOT EXISTS(SELECT 1 FROM photo.model_qualifications q
   WHERE q.id=result_id AND q.report_sha=p_sha AND q.trusted_import
    AND q.operation=policy_row.operation
    AND q.operation_version=policy_row.operation_version
    AND q.model_lock_sha=policy_row.model_lock_sha
    AND q.runtime_lock_sha=policy_row.runtime_lock_sha
    AND q.backend=policy_row.backend
    AND q.device_profile_sha=policy_row.device_profile_sha) THEN
   RAISE EXCEPTION 'QUALIFICATION_REPLAY_CONFLICT' USING ERRCODE='40001';
  END IF;
  RETURN result_id;
 END IF;
 INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,
  operation_version,model_lock_sha,runtime_lock_sha,backend,
  device_profile_sha,dataset_sha,report_artifact_id,report_sha,
  trusted_import,status)
 VALUES(p_tenant,p_project,policy_row.operation,policy_row.operation_version,
  policy_row.model_lock_sha,policy_row.runtime_lock_sha,policy_row.backend,
  policy_row.device_profile_sha,policy_row.dataset_sha,p_report,p_sha,
  true,'passed') RETURNING id INTO result_id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,
  resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_operator,'v2_qualification.imported',
  'model_qualification',result_id,gen_random_uuid(),
  jsonb_build_object('project_id',p_project,'report_artifact_id',p_report,
   'report_sha',p_sha,'policy_id',policy_row.id));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1',
  'v2.qualification.imported',jsonb_build_object('schema_version',1,
   'event_id',event_id,'type','v2.qualification.imported',
   'tenant_id',p_tenant,'project_id',p_project,
   'qualification_id',result_id));
 RETURN result_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.import_v2_qualification(uuid,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.import_v2_qualification(uuid,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.import_v2_qualification(uuid,uuid,uuid,text,uuid,text,text)
 TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
