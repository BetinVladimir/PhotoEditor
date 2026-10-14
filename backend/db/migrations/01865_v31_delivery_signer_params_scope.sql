-- +goose Up
-- Read-only authority for publishing one task_params document beneath the
-- delegated signer draft. The caller must still reserve and complete through
-- dedicated scheduler-only functions; this function grants no direct DML.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_signer_params_scope(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;actor uuid;project uuid;export_id uuid;
 wid uuid;rid uuid;sid uuid;tid uuid;policy_id uuid;policy_ref uuid;
 params_ref uuid;params_sha text;lineage_ref uuid;lineage_sha text;
 params jsonb;expected_inputs jsonb;request_sha text;
 r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;s photo.snapshots%ROWTYPE;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
  OR p_lineage_bytes IS NULL OR p_lineage_bytes NOT BETWEEN 2 AND 8388608
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
  OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL THEN RETURN NULL;END IF;
 tenant:=(scope->>'tenant_id')::uuid;actor:=(scope->>'actor_id')::uuid;
 project:=(scope->>'project_id')::uuid;export_id:=(scope->>'export_id')::uuid;
 wid:=(scope->>'workflow_id')::uuid;rid:=photo.v30_uuid5(wid,'run');
 sid:=photo.v30_uuid5(wid,'provenance:artifact-snapshot:1');
 tid:=(scope->>'task_id')::uuid;policy_id:=(scope->>'policy_id')::uuid;
 policy_ref:=photo.v30_uuid5(tenant,
  'signing:policy:'||policy_id::text||':'||(scope->>'policy_sha'));
 params_ref:=photo.v30_uuid5(tid,'params');
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 params:=p_params::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object'
  OR params->>'schema_version' IS DISTINCT FROM '1'
  OR params->>'signing_policy_id' IS DISTINCT FROM policy_id::text
  OR params->'export' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',scope->>'export_artifact_id','sha256',scope->>'export_sha')
  OR params->'policy' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',policy_ref,'sha256',scope->>'policy_sha')
  THEN RETURN NULL;END IF;
 lineage_ref:=(params->'lineage'->>'artifact_id')::uuid;
 lineage_sha:=params->'lineage'->>'sha256';
 IF lineage_sha IS NULL OR lineage_sha !~ '^[0-9a-f]{64}$'
  OR lineage_ref IS DISTINCT FROM photo.v30_uuid5(export_id,
   'signing:lineage:'||lineage_sha)
  OR photo.v31_delivery_lineage_ready(p_expansion,p_token,p_ordinal,
   p_preset,p_source,lineage_ref,lineage_sha,p_lineage_bytes,p_bucket)
   IS DISTINCT FROM true
  OR photo.v31_signing_policy_ready(tenant,policy_id,policy_ref,
   scope->>'policy_sha') IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',entry.ref,'contract',entry.contract)
  ORDER BY (entry.ref->>'artifact_id')::uuid) INTO expected_inputs
 FROM (VALUES(params->'export','QualifiedExport'),
  (params->'lineage','SigningLineage'),
  (params->'policy','SigningPolicy')) AS entry(ref,contract);
 request_sha:=encode(sha256(convert_to(format(
  '{"export_id":"%s","signing_policy_id":"%s"}',export_id,policy_id),
  'UTF8')),'hex');
 SELECT * INTO r FROM photo.runs WHERE tenant_id=tenant AND id=rid
  AND kind='v2' AND status='draft' AND NOT graph_sealed
  AND NOT cancel_requested AND created_by=actor FOR SHARE;
 IF NOT FOUND OR r.input_snapshot_id IS DISTINCT FROM sid
  OR r.failure_policy IS DISTINCT FROM 'fail_fast'
  OR r.config_snapshot->>'workflow_type' IS DISTINCT FROM 'provenance'
  OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text
  OR r.config_snapshot->>'export_id' IS DISTINCT FROM export_id::text
  OR r.config_snapshot->>'request_sha' IS DISTINCT FROM request_sha
  OR r.config_snapshot->>'queue_class' IS DISTINCT FROM 'batch'
  OR r.config_snapshot->>'snapshot_id' IS DISTINCT FROM sid::text
  OR r.config_snapshot->'params' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',params_ref,'sha256',params_sha)
  THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=tenant AND run_id=rid
  AND id=wid AND project_id=project AND workflow_type='provenance'
  AND renderer_version='v2.2' AND policy_sha=scope->>'policy_sha'
  AND input_snapshot_id=sid FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=tenant AND id=sid
  AND kind='v2' AND principal_type='user' AND principal_id=actor
  AND item_count=3 AND expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND OR r.config_snapshot->>'snapshot_digest' IS DISTINCT FROM s.digest
  OR (SELECT jsonb_agg(x.payload ORDER BY x.ordinal)
   FROM photo.snapshot_items x WHERE x.tenant_id=tenant
    AND x.snapshot_id=sid) IS DISTINCT FROM expected_inputs THEN RETURN NULL;END IF;
 RETURN rid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_signer_params_scope(uuid,uuid,integer,uuid,jsonb,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_signer_params_scope(uuid,uuid,integer,uuid,jsonb,bigint,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
