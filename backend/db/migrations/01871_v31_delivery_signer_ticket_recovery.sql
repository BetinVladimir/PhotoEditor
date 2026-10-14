-- +goose Up
-- Recover the committed creation acknowledgement of one Delivery-delegated
-- signer. This does not claim signing success or inspect mutable worker output.
-- +goose StatementBegin
CREATE FUNCTION photo.recover_v31_delivery_signer_ticket(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE status text;observed_export uuid;observed_task uuid;
 e photo.graph_expansions%ROWTYPE;parent photo.runs%ROWTYPE;
 package photo.delivery_packages%ROWTYPE;item photo.delivery_items%ROWTYPE;
 frozen jsonb;policy uuid;policy_sha text;child_key text;
 wid uuid;rid uuid;tid uuid;sid uuid;
BEGIN
 SELECT signing_status,export_id,task_id INTO status,observed_export,observed_task
  FROM photo.read_v30_delivery_signer_status(
   p_expansion,p_token,p_ordinal,p_preset);
 IF status IS NULL OR status='not_started' THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion
  AND kind='v2.delivery/1' AND state='open' AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=e.tenant_id
  AND id=e.run_id AND kind='v2' AND status='running'
  AND NOT cancel_requested AND created_by IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO package FROM photo.delivery_packages WHERE tenant_id=e.tenant_id
  AND workflow_id=photo.v30_uuid5(e.run_id,'workflow')
  AND state='building' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO item FROM photo.delivery_items WHERE tenant_id=e.tenant_id
  AND package_id=package.id AND project_id=package.project_id
  AND ordinal=p_ordinal AND preset_id=p_preset AND state='ready'
  AND export_id=observed_export FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.projects p
  WHERE p.tenant_id=e.tenant_id AND p.id=package.project_id
   AND p.archived_at IS NULL AND p.download_allowed) THEN RETURN NULL;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
  FROM photo.backend_documents b WHERE b.tenant_id=e.tenant_id
   AND b.run_id=e.run_id AND b.owner_id=e.parent_task_id
   AND b.purpose='task_params' AND b.state='ready'
   AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR coalesce(frozen->'policy'->>'signing_policy_sha','') !~ '^[0-9a-f]{64}$'
  THEN RETURN NULL;END IF;
 policy:=(frozen->'policy'->>'signing_policy_id')::uuid;
 policy_sha:=frozen->'policy'->>'signing_policy_sha';
 IF policy IS NULL OR photo.v31_signing_policy_ready(e.tenant_id,policy,
  photo.v30_uuid5(e.tenant_id,'signing:policy:'||policy::text||':'||policy_sha),
  policy_sha) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 child_key:=photo.v30_uuid5(e.run_id,
  p_ordinal::text||':'||p_preset::text)::text;
 wid:=photo.v30_uuid5(e.tenant_id,
  'provenance:'||parent.created_by::text||':'||child_key);
 rid:=photo.v30_uuid5(wid,'run');
 tid:=photo.v30_uuid5(wid,'provenance.sign:0');
 sid:=photo.v30_uuid5(wid,'provenance:artifact-snapshot:1');
 IF observed_task IS DISTINCT FROM tid OR NOT EXISTS(
  SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
   ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
   AND t.id=tid
  JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id
   AND d.run_id=t.run_id AND d.owner_id=t.id AND d.purpose='task_params'
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE r.tenant_id=e.tenant_id AND r.id=rid AND r.kind='v2'
   AND r.graph_sealed AND r.status<>'draft' AND r.created_by=parent.created_by
   AND r.input_snapshot_id=sid AND r.config_snapshot->>'workflow_type'='provenance'
   AND r.config_snapshot->>'project_id'=package.project_id::text
   AND r.config_snapshot->>'export_id'=item.export_id::text
   AND r.config_snapshot->>'snapshot_id'=sid::text
   AND r.config_snapshot->'params' IS NOT DISTINCT FROM jsonb_build_object(
    'artifact_id',d.artifact_id,'sha256',d.sha256)
   AND w.id=wid AND w.project_id=package.project_id
   AND w.input_snapshot_id=sid AND w.workflow_type='provenance'
   AND w.renderer_version='v2.2' AND w.policy_sha=policy_sha
   AND t.node_key='provenance.sign:0' AND t.operation='provenance.sign'
   AND t.operation_version='1' AND t.resource_class='cpu'
   AND t.params_artifact_id=d.artifact_id
   AND t.execution_profile->>'execution_pool'='signer'
   AND t.execution_profile->>'v2_entry_count'='3'
   AND t.execution_profile->>'v2_entries_sha'=t.input_digest
   AND jsonb_typeof(t.execution_profile->'v2_input_manifest')='object'
   AND d.state='ready' AND d.sha256=a.sha256 AND d.bytes=a.bytes
   AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.media_type='application/json' AND a.output_role='task_params'
   AND a.object_key='tenants/'||e.tenant_id||'/runs/'||rid||'/documents/'||d.artifact_id
   AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v2_input_closure_ready(tid)
 ) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.recover_v31_delivery_signer_ticket(uuid,uuid,integer,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_v31_delivery_signer_ticket(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.recover_v31_delivery_signer_ticket(uuid,uuid,integer,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
