-- +goose Up
-- Atomic scheduler activation of the exact delegated signer child. The
-- established V2 seal retains registry, quota, closure and outbox gates.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v31_delivery_signer(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text,
 p_task jsonb,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;actor uuid;project uuid;wid uuid;rid uuid;
 tid uuid;pid uuid;params jsonb;params_sha text;lineage_id uuid;lineage_sha text;
 r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object'
  OR jsonb_typeof(p_manifest) IS DISTINCT FROM 'object'
  OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL THEN RETURN false;END IF;
 tenant:=(scope->>'tenant_id')::uuid;actor:=(scope->>'actor_id')::uuid;
 project:=(scope->>'project_id')::uuid;wid:=(scope->>'workflow_id')::uuid;
 rid:=photo.v30_uuid5(wid,'run');tid:=(scope->>'task_id')::uuid;
 pid:=photo.v30_uuid5(tid,'params');
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 params:=p_params::jsonb;
 lineage_id:=(params->'lineage'->>'artifact_id')::uuid;
 lineage_sha:=params->'lineage'->>'sha256';
 IF params->>'schema_version' IS DISTINCT FROM '1'
  OR params->>'signing_policy_id' IS DISTINCT FROM scope->>'policy_id'
  OR params->'export' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',scope->>'export_artifact_id','sha256',scope->>'export_sha')
  OR params->'policy' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',photo.v30_uuid5(tenant,'signing:policy:'||
    (scope->>'policy_id')||':'||(scope->>'policy_sha')),
   'sha256',scope->>'policy_sha')
  OR lineage_sha IS NULL OR lineage_sha !~ '^[0-9a-f]{64}$'
  OR lineage_id IS DISTINCT FROM photo.v30_uuid5(
   (scope->>'export_id')::uuid,'signing:lineage:'||lineage_sha)
  OR photo.v31_delivery_lineage_ready(p_expansion,p_token,p_ordinal,
   p_preset,p_source,lineage_id,lineage_sha,p_lineage_bytes,p_bucket)
   IS DISTINCT FROM true
  OR p_task->>'id' IS DISTINCT FROM tid::text
  OR p_task->>'operation' IS DISTINCT FROM 'provenance.sign'
  OR p_task->>'operation_version' IS DISTINCT FROM '1'
  OR p_task->>'node_key' IS DISTINCT FROM 'provenance.sign:0'
  OR p_task->>'params_artifact_id' IS DISTINCT FROM pid::text
  OR p_task->>'params_sha' IS DISTINCT FROM params_sha
  OR p_manifest->>'artifact_id' IS NULL
  OR coalesce(p_manifest->>'sha256','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=tenant AND id=rid FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=tenant AND run_id=rid FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=tenant AND id=tid
  AND run_id=rid FOR UPDATE;
 IF r.id IS NULL OR w.id IS DISTINCT FROM wid OR t.id IS NULL
  OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM actor
  OR r.cancel_requested OR w.project_id IS DISTINCT FROM project
  OR w.workflow_type IS DISTINCT FROM 'provenance'
  OR w.renderer_version IS DISTINCT FROM 'v2.2'
  OR w.policy_sha IS DISTINCT FROM scope->>'policy_sha'
  OR r.config_snapshot->>'export_id' IS DISTINCT FROM scope->>'export_id'
  OR r.config_snapshot->'params' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',pid,'sha256',params_sha)
  OR t.operation IS DISTINCT FROM 'provenance.sign'
  OR t.operation_version IS DISTINCT FROM '1'
  OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
  OR t.params_artifact_id IS DISTINCT FROM pid
  OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM p_manifest
  OR t.execution_profile->>'v2_entry_count' IS DISTINCT FROM '3'
  OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM p_task->>'input_digest'
  OR photo.v2_input_closure_ready(tid) IS DISTINCT FROM true
  OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=tenant AND run_id=rid)<>1
  OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=tenant AND run_id=rid)
  THEN RETURN false;END IF;
 IF r.graph_sealed THEN
  RETURN r.status IN('queued','running') AND t.state IN('ready','leased','running','succeeded')
   AND photo.v30_delivery_signer_delegation_scope(p_expansion,p_token,
    p_ordinal,p_preset) IS NOT DISTINCT FROM scope;
 END IF;
 IF r.status IS DISTINCT FROM 'draft' OR t.state IS DISTINCT FROM 'blocked'
  OR t.attempt<>0 OR t.fencing_token<>0 OR t.cancel_requested
  OR t.canonical_result_id IS NOT NULL THEN RETURN false;END IF;
 IF photo.v31_delivery_signer_transport_ready(p_expansion,p_token,
  p_ordinal,p_preset,p_source,p_lineage_bytes,p_params,p_bucket,p_task)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.seal_v2_graph_pre_v31_signing(tenant,rid) IS DISTINCT FROM true
  THEN RETURN false;END IF;
 IF photo.v30_delivery_signer_delegation_scope(p_expansion,p_token,
  p_ordinal,p_preset) IS DISTINCT FROM scope THEN
  RAISE EXCEPTION 'SIGNER_DELEGATION_STALE' USING ERRCODE='23514';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_v31_delivery_signer(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v31_delivery_signer(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.activate_v31_delivery_signer(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb,jsonb)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
