-- +goose Up
-- Current signer scope before generic scheduler registration. No activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_transport_ready(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;rid uuid;tid uuid;pid uuid;expected_profile jsonb;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Provenance.Sign.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR p_task IS DISTINCT FROM saved->'task' THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 tid:=(saved->>'task_id')::uuid;pid:=photo.v30_uuid5(tid,'params');expected_profile:=p_task->'execution_profile';
 rid:=photo.v31_signing_params_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,tid,pid,p_task->>'params_sha',octet_length(saved->>'params_jcs'),saved->>'bucket');
 IF rid IS NULL THEN RETURN false;END IF;
 PERFORM t.id FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id AND d.purpose='task_params' AND d.artifact_id=a.id
 WHERE t.tenant_id=p_tenant AND t.id=tid AND t.run_id=rid AND t.state='blocked' AND t.attempt=0 AND t.fencing_token=0 AND NOT t.cancel_requested AND t.canonical_result_id IS NULL
 AND t.node_key='provenance.sign:0' AND t.operation='provenance.sign' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.params_artifact_id=pid AND t.input_digest=p_task->>'input_digest'
 AND (t.execution_profile=expected_profile OR (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'=expected_profile AND photo.v2_input_closure_ready(tid)))
 AND d.state='ready' AND d.sha256=p_task->>'params_sha' AND d.bytes=octet_length(saved->>'params_jcs') AND d.bucket=saved->>'bucket'
 AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||pid AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=rid AND q.principal_type='user' AND q.principal_id=p_actor
  AND q.state='reserved' AND q.bytes=(expected_profile->'resource_estimate'->>'output_bytes')::bigint AND q.expires_at>clock_timestamp()) FOR UPDATE OF t FOR SHARE OF a,d;
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.v31_signing_transport_ready(uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_transport_ready(uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_transport_ready(uuid,uuid,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
