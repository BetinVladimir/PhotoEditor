-- +goose Up
-- Current delegated signer authority before generic V2 input registration.
-- This function never registers or activates a task by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_signer_transport_ready(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text,
 p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;actor uuid;rid uuid;tid uuid;pid uuid;
 expected_profile jsonb;params_sha text;output bigint;
BEGIN
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object'
  OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 tenant:=(scope->>'tenant_id')::uuid;actor:=(scope->>'actor_id')::uuid;
 tid:=(scope->>'task_id')::uuid;pid:=photo.v30_uuid5(tid,'params');
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 expected_profile:=p_task->'execution_profile';
 output:=(expected_profile->'resource_estimate'->>'output_bytes')::bigint;
 IF output IS NULL OR output NOT BETWEEN 553648128 AND 9007199254740991
  OR p_task->>'id' IS DISTINCT FROM tid::text
  OR p_task->>'params_artifact_id' IS DISTINCT FROM pid::text
  OR p_task->>'params_sha' IS DISTINCT FROM params_sha
  OR p_task->>'operation' IS DISTINCT FROM 'provenance.sign'
  OR p_task->>'node_key' IS DISTINCT FROM 'provenance.sign:0'
  OR p_task->>'operation_version' IS DISTINCT FROM '1'
  OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
  OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 rid:=photo.v31_delivery_signer_params_scope(p_expansion,p_token,
  p_ordinal,p_preset,p_source,p_lineage_bytes,p_params,p_bucket);
 IF rid IS NULL OR photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset) IS DISTINCT FROM scope
  OR p_task->>'input_digest' IS DISTINCT FROM (
   SELECT s.digest FROM photo.snapshots s JOIN photo.runs r
    ON r.tenant_id=s.tenant_id AND r.input_snapshot_id=s.id
    WHERE r.tenant_id=tenant AND r.id=rid) THEN RETURN false;END IF;
 PERFORM t.id FROM photo.tasks t JOIN photo.artifacts a
  ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id
  AND d.run_id=t.run_id AND d.owner_id=t.id
  AND d.purpose='task_params' AND d.artifact_id=a.id
 WHERE t.tenant_id=tenant AND t.id=tid AND t.run_id=rid
  AND t.state='blocked' AND t.attempt=0 AND t.fencing_token=0
  AND NOT t.cancel_requested AND t.canonical_result_id IS NULL
  AND t.node_key='provenance.sign:0' AND t.operation='provenance.sign'
  AND t.operation_version='1' AND t.resource_class='cpu'
  AND t.params_artifact_id=pid AND t.input_digest=p_task->>'input_digest'
  AND (t.execution_profile=expected_profile OR
   (t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'=expected_profile
    AND photo.v2_input_closure_ready(tid)))
  AND d.state='ready' AND d.sha256=params_sha
  AND d.bytes=octet_length(p_params) AND d.bucket=p_bucket
  AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_version=d.object_version
  AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role='task_params'
  AND a.object_key='tenants/'||tenant||'/runs/'||rid||'/documents/'||pid
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=tenant AND q.holder_type='run' AND q.holder_id=rid
    AND q.principal_type='user' AND q.principal_id=actor
    AND q.state='reserved' AND q.bytes=output
    AND q.expires_at>clock_timestamp())
 FOR UPDATE OF t FOR SHARE OF a,d;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_signer_transport_ready(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_signer_transport_ready(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_signer_transport_ready(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
