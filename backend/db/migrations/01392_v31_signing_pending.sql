-- +goose Up
-- Durable preparation only. No draft/task creation, quota reservation or dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.pin_v31_signing_pending(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_body text)
RETURNS TABLE(outcome text) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;request jsonb;params jsonb;ledger photo.idempotency_records%ROWTYPE;rid uuid;wid uuid;tid uuid;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$'
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$'
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 10485760 THEN RETURN QUERY SELECT 'invalid';RETURN;END IF;
 BEGIN saved:=p_body::jsonb;request:=(saved->>'request_jcs')::jsonb;params:=(saved->>'params_jcs')::jsonb;
 EXCEPTION WHEN invalid_text_representation THEN RETURN QUERY SELECT 'invalid';RETURN;END;
 IF jsonb_typeof(saved) IS DISTINCT FROM 'object' OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text
 OR saved->>'request_sha' IS DISTINCT FROM p_request_sha OR saved->>'request_jcs' IS NULL
 OR encode(sha256(convert_to(saved->>'request_jcs','UTF8')),'hex') IS DISTINCT FROM p_request_sha
 OR saved->>'params_jcs' IS NULL OR saved->>'lineage_jcs' IS NULL
 OR octet_length(saved->>'params_jcs') NOT BETWEEN 2 AND 65536
 OR octet_length(saved->>'lineage_jcs') NOT BETWEEN 2 AND 8388608
 OR encode(sha256(convert_to(saved->>'lineage_jcs','UTF8')),'hex') IS DISTINCT FROM params->'lineage'->>'sha256'
 OR request->>'export_id' IS DISTINCT FROM saved->>'export_id'
 OR request->>'signing_policy_id' IS DISTINCT FROM params->>'signing_policy_id' THEN RETURN QUERY SELECT 'invalid';RETURN;END IF;
 wid:=photo.v30_uuid5(p_tenant,'provenance:'||p_actor||':'||p_key);rid:=photo.v30_uuid5(wid,'run');tid:=photo.v30_uuid5(wid,'provenance.sign:0');
 IF saved->>'workflow_id' IS DISTINCT FROM wid::text OR saved->>'run_id' IS DISTINCT FROM rid::text
 OR saved->>'task_id' IS DISTINCT FROM tid::text OR saved->'task'->>'id' IS DISTINCT FROM tid::text
 OR saved->'task'->>'operation' IS DISTINCT FROM 'provenance.sign'
 OR saved->'task'->>'operation_version' IS DISTINCT FROM '1' OR saved->'task'->>'resource_class' IS DISTINCT FROM 'cpu'
 OR saved->'task'->'execution_profile'->>'execution_pool' IS DISTINCT FROM 'signer'
 OR saved->'task'->>'params_artifact_id' IS DISTINCT FROM photo.v30_uuid5(tid,'params')::text
 OR saved->'task'->>'params_sha' IS DISTINCT FROM encode(sha256(convert_to(saved->>'params_jcs','UTF8')),'hex')
 THEN RETURN QUERY SELECT 'invalid';RETURN;END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(p_tenant,'user',p_actor,'Provenance.Sign.Pending',p_key,p_request_sha,clock_timestamp()+interval '24 hours')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO ledger FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user'
 AND x.principal_id=p_actor AND x.route='Provenance.Sign.Pending' AND x.key=p_key FOR UPDATE;
 IF ledger.request_hash IS DISTINCT FROM p_request_sha THEN RETURN QUERY SELECT 'conflict';RETURN;END IF;
 IF ledger.expires_at<=clock_timestamp() THEN RETURN QUERY SELECT 'expired';RETURN;END IF;
 IF ledger.response_status IS NOT NULL THEN RETURN QUERY SELECT 'completed';RETURN;END IF;
 IF ledger.response_body IS NOT NULL AND ledger.response_body IS DISTINCT FROM saved THEN RETURN QUERY SELECT 'conflict';RETURN;END IF;
 IF photo.v31_signing_admission_ready(p_tenant,p_actor,p_session,(saved->>'export_id')::uuid,saved->'source_pins',saved->>'bucket',saved->>'params_jcs') IS DISTINCT FROM true THEN RETURN QUERY SELECT 'stale';RETURN;END IF;
 IF ledger.response_body IS NOT NULL THEN RETURN QUERY SELECT 'pending';RETURN;END IF;
 UPDATE photo.idempotency_records SET response_body=saved,resource_id=rid WHERE id=ledger.id;
 RETURN QUERY SELECT 'created';
END $$;
ALTER FUNCTION photo.pin_v31_signing_pending(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v31_signing_pending(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.pin_v31_signing_pending(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
