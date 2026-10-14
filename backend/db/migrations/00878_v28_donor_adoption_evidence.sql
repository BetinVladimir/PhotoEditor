-- +goose Up
-- Scheduler current source/qualification evidence after canonical completion.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_adoption_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;receipt jsonb;current_receipt jsonb;plan jsonb;proof text;context record;previous_tenant text;
BEGIN
 SELECT * INTO context FROM photo.v2_adoption_context(p_tenant,p_task);
 IF NOT FOUND OR context.attempt IS DISTINCT FROM p_attempt OR context.fence IS DISTINCT FROM p_fence
 OR context.result_id IS DISTINCT FROM p_result OR context.operation IS DISTINCT FROM 'portrait.open_eyes'
 OR context.operation_version IS DISTINCT FROM '1' THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 receipt:=r.v28_donor_queued_receipt;
 IF receipt IS NULL OR r.v28_donor_descriptor_jcs IS NULL THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 current_receipt:=photo.read_v28_donor_queued_receipt(p_tenant,r.id,
 (receipt->>'actor_id')::uuid,(receipt->>'auth_session_id')::uuid);
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF current_receipt IS DISTINCT FROM receipt THEN RETURN NULL;END IF;
 plan:=r.v28_donor_descriptor_jcs::jsonb;
 SELECT v28_donor_issuer_context->>'proof_jcs' INTO proof FROM photo.backend_documents
 WHERE tenant_id=p_tenant AND run_id=r.id AND state='ready'
 AND artifact_id=(plan->'params'->'confirmation'->>'artifact_id')::uuid
 AND sha256=plan->'params'->'confirmation'->>'sha256';
 IF proof IS NULL OR t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'run_id',r.id,'task_id',t.id,
 'attempt',p_attempt,'fence',p_fence,'result_id',p_result,
 'params_ref',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',plan->'task'->>'params_sha'),
 'params',plan->'params','proof_jcs',proof,'profile',t.execution_profile,
 'manual',receipt->'manual','qualification_objects',plan->'qualification_objects');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_donor_adoption_evidence(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_adoption_evidence(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_donor_adoption_evidence(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
