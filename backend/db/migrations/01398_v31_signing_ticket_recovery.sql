-- +goose Up
-- Recover only a committed creation acknowledgement. Does not assert job success
-- and does not depend on current SDK availability or expired source inputs.
-- +goose StatementBegin
CREATE FUNCTION photo.recover_v31_signing_ticket(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_policy uuid,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,ticket jsonb) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ledger photo.idempotency_records%ROWTYPE;saved jsonb;expected jsonb;rid uuid;wid uuid;tid uuid;project uuid;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_export IS NULL OR p_policy IS NULL OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$'
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'invalid',NULL::jsonb;RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.authorize_tenant(p_actor,p_session,p_tenant) a WHERE a.role IN('owner','admin','editor')) THEN RETURN QUERY SELECT 'forbidden',NULL::jsonb;RETURN;END IF;
 SELECT * INTO ledger FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user' AND x.principal_id=p_actor
 AND x.route='Provenance.Sign.Pending' AND x.key=p_key FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb;RETURN;END IF;
 IF ledger.request_hash IS DISTINCT FROM p_request_sha THEN RETURN QUERY SELECT 'conflict',NULL::jsonb;RETURN;END IF;
 IF ledger.expires_at<=clock_timestamp() THEN RETURN QUERY SELECT 'expired',NULL::jsonb;RETURN;END IF;
 IF ledger.response_status IS NULL THEN RETURN QUERY SELECT 'pending',NULL::jsonb;RETURN;END IF;
 IF ledger.response_status<>202 OR ledger.response_body->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN QUERY SELECT 'invalid',NULL::jsonb;RETURN;END IF;
 saved:=ledger.response_body->'frozen';
 wid:=photo.v30_uuid5(p_tenant,'provenance:'||p_actor||':'||p_key);rid:=photo.v30_uuid5(wid,'run');tid:=photo.v30_uuid5(wid,'provenance.sign:0');
 IF saved IS NULL OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text
 OR saved->>'export_id' IS DISTINCT FROM p_export::text OR saved->>'workflow_id' IS DISTINCT FROM wid::text OR saved->>'run_id' IS DISTINCT FROM rid::text
 OR saved->>'task_id' IS DISTINCT FROM tid::text OR saved->>'request_sha' IS DISTINCT FROM p_request_sha
 OR encode(sha256(convert_to(saved->>'request_jcs','UTF8')),'hex') IS DISTINCT FROM p_request_sha
 OR (saved->>'request_jcs')::jsonb IS DISTINCT FROM jsonb_build_object('export_id',p_export,'signing_policy_id',p_policy)
 OR ledger.resource_id IS DISTINCT FROM rid THEN RETURN QUERY SELECT 'invalid',NULL::jsonb;RETURN;END IF;
 project:=(saved->'source_pins'->>'project')::uuid;
 IF NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) OR NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=project AND p.archived_at IS NULL AND p.download_allowed) THEN RETURN QUERY SELECT 'forbidden',NULL::jsonb;RETURN;END IF;
 expected:=jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
 IF ledger.response_body->'ticket' IS DISTINCT FROM expected OR NOT EXISTS(
 SELECT FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=tid
 WHERE r.tenant_id=p_tenant AND r.id=rid AND r.kind='v2' AND r.graph_sealed AND r.status<>'draft' AND r.created_by=p_actor
 AND r.config_snapshot->>'request_sha'=p_request_sha AND r.config_snapshot->>'export_id'=p_export::text
 AND r.config_snapshot->'params'->>'artifact_id'=t.params_artifact_id::text
 AND r.config_snapshot->'params'->>'sha256'=saved->'task'->>'params_sha'
 AND w.id=wid AND w.project_id=project AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='provenance' AND w.renderer_version='v2.2'
 AND t.operation='provenance.sign' AND t.operation_version='1' AND t.resource_class='cpu' AND t.execution_profile->>'execution_pool'='signer'
 AND t.input_digest=saved->'task'->>'input_digest' AND t.params_artifact_id=(saved->'task'->>'params_artifact_id')::uuid
 ) THEN RETURN QUERY SELECT 'invalid',NULL::jsonb;RETURN;END IF;
 RETURN QUERY SELECT 'replayed',expected;
END $$;
ALTER FUNCTION photo.recover_v31_signing_ticket(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_v31_signing_ticket(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.recover_v31_signing_ticket(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
