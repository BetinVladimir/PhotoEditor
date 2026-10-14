-- +goose Up
-- Operator-provisioned qualification only; no worker/API self-attestation grant.
ALTER TABLE photo.worker_registrations
 ADD COLUMN execution_pool text NOT NULL DEFAULT 'general' CHECK(execution_pool IN('general','signer')),
 ADD COLUMN signer_sdk_lock_sha text,
 ADD COLUMN signer_certificate_chain_sha text,
 ADD COLUMN signer_trust_bundle_sha text,
 ADD COLUMN signer_qualification_sha text,
 ADD COLUMN signer_qualified_until timestamptz,
 ADD CONSTRAINT v31_signer_registration CHECK (
 (execution_pool='general' AND signer_sdk_lock_sha IS NULL AND signer_certificate_chain_sha IS NULL
 AND signer_trust_bundle_sha IS NULL AND signer_qualification_sha IS NULL AND signer_qualified_until IS NULL
 AND NOT qualified_operations ? 'provenance.sign') OR
 (execution_pool='signer' AND signer_sdk_lock_sha IS NOT NULL AND signer_sdk_lock_sha ~ '^[0-9a-f]{64}$'
 AND signer_certificate_chain_sha IS NOT NULL AND signer_certificate_chain_sha ~ '^[0-9a-f]{64}$'
 AND signer_trust_bundle_sha IS NOT NULL AND signer_trust_bundle_sha ~ '^[0-9a-f]{64}$'
 AND signer_qualification_sha IS NOT NULL AND signer_qualification_sha ~ '^[0-9a-f]{64}$'
 AND signer_qualified_until IS NOT NULL AND isfinite(signer_qualified_until)
 AND resource_classes='["cpu"]'::jsonb AND backends='["cpu"]'::jsonb
 AND qualified_operations='{"provenance.sign":["cpu"]}'::jsonb));
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signer_worker_matches(p_tenant uuid,p_worker uuid,p_operation text,p_profile jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.enabled
 AND CASE WHEN p_operation='provenance.sign' THEN
 w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 AND p_profile->>'execution_pool'='signer' AND p_profile->>'backend'='cpu' AND p_profile->>'renderer_version'='v2.2'
 AND p_profile->>'sdk_lock_sha'=w.signer_sdk_lock_sha
 AND p_profile->>'certificate_chain_sha'=w.signer_certificate_chain_sha
 AND p_profile->>'trust_bundle_sha'=w.signer_trust_bundle_sha
 ELSE w.execution_pool='general' AND coalesce(p_profile->>'execution_pool','general')<>'signer' END);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_signer_worker_matches(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signer_worker_matches(uuid,uuid,text,jsonb) FROM PUBLIC;
-- Authenticated read-only probe; never creates or refreshes qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.signer_runtime_ready(p_worker uuid,p_sdk text,p_certificate text,p_trust text,p_qualification text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker AND w.db_principal=session_user
 AND w.enabled AND w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 AND w.signer_sdk_lock_sha=p_sdk AND w.signer_certificate_chain_sha=p_certificate
 AND w.signer_trust_bundle_sha=p_trust AND w.signer_qualification_sha=p_qualification);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.signer_runtime_ready(uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.signer_runtime_ready(uuid,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.signer_runtime_ready(uuid,text,text,text,text) TO photo_worker;
-- Preserve complete installed fairness/resource/model/V2 dispatch definitions.
-- Require exactly one known worker freshness predicate; schema drift fails migration.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text:='w.last_seen>clock_timestamp()';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.eligible_dispatch_nodes(uuid)',
 'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
 'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected worker selection definition: %',signature;
  END IF;
  definition:=replace(definition,needle,
   'photo.v31_signer_worker_matches(t.tenant_id,w.worker_id,t.operation,t.execution_profile) AND '||needle);
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_pre_v31_signer;
REVOKE ALL ON FUNCTION photo.claim_task_pre_v31_signer(uuid,integer,uuid,jsonb) FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;claimed record;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.id IS NOT NULL AND photo.v31_signer_worker_matches(t.tenant_id,p_worker,t.operation,t.execution_profile) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb;RETURN;
 END IF;
 IF t.operation='provenance.sign' THEN
  SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id FOR UPDATE;
  IF t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
   OR r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR NOT r.graph_sealed OR r.cancel_requested
   OR r.status NOT IN('queued','running')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=t.tenant_id AND w.run_id=t.run_id
    AND w.workflow_type='provenance' AND w.renderer_version='v2.2') THEN
   RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb;RETURN;
  END IF;
 END IF;
 SELECT * INTO claimed FROM photo.claim_task_pre_v31_signer(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' AND t.operation='provenance.sign' AND r.status='queued' THEN
  IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
  UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=t.tenant_id AND id=t.run_id;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
ALTER FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) RENAME TO worker_authorized_pre_v31_signer;
REVOKE ALL ON FUNCTION photo.worker_authorized_pre_v31_signer(uuid,uuid,text,text,text,text,text) FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.worker_authorized(p_tenant uuid,p_worker uuid,p_class text,p_node text,p_operation text,p_backend text,p_lock_id text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.worker_authorized_pre_v31_signer(p_tenant,p_worker,p_class,p_node,p_operation,p_backend,p_lock_id)
 AND EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
 AND CASE WHEN p_operation='provenance.sign' THEN w.execution_pool='signer' AND w.signer_qualified_until>clock_timestamp()
 ELSE w.execution_pool='general' END);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
