-- +goose Up
-- Scheduler-only observation of one frozen ready Delivery slot's canonical
-- V31 signer. `adopted` is SQL evidence, never physical byte verification.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_delivery_signer_status(p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS TABLE(signing_status text,export_id uuid,task_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint_tenant uuid;hint_run uuid;x photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;
 signer_workflow uuid;signer_run uuid;signer_task uuid;signer_key text;t photo.tasks%ROWTYPE;sr photo.runs%ROWTYPE;sw photo.v2_workflows%ROWTYPE;frozen jsonb;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_preset IS NULL THEN RETURN;END IF;
 SELECT tenant_id,run_id INTO hint_tenant,hint_run FROM photo.graph_expansions
  WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=hint_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=hint_tenant AND id=hint_run
  AND status='running' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND tenant_id=hint_tenant
  AND run_id=hint_run AND kind='v2.delivery/1' AND state='open'
  AND expansion_token=p_token AND expansion_lease_until>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND workflow_type='delivery' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=x.tenant_id AND workflow_id=w.id
  AND project_id=w.project_id AND state='building' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=x.tenant_id AND package_id=d.id
  AND project_id=d.project_id AND ordinal=p_ordinal AND preset_id=p_preset
  AND state='ready' AND export_id IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_id' IS NULL
  OR frozen->'policy'->>'signing_policy_sha' IS NULL
  OR frozen->'policy'->>'signing_policy_sha' !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 signer_key:=photo.v30_uuid5(x.run_id,p_ordinal::text||':'||p_preset::text)::text;
 signer_workflow:=photo.v30_uuid5(x.tenant_id,'provenance:'||r.created_by::text||':'||signer_key);
 signer_run:=photo.v30_uuid5(signer_workflow,'run');
 signer_task:=photo.v30_uuid5(signer_workflow,'provenance.sign:0');
 SELECT * INTO sw FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND id=signer_workflow FOR SHARE;
 IF NOT FOUND THEN
  RETURN QUERY SELECT 'not_started'::text,i.export_id,signer_task;RETURN;
 END IF;
 IF sw.run_id<>signer_run OR sw.project_id<>d.project_id OR sw.workflow_type<>'provenance'
  OR sw.policy_sha IS DISTINCT FROM frozen->'policy'->>'signing_policy_sha'
  OR sw.renderer_version<>'v2.2' THEN RETURN;END IF;
 SELECT * INTO sr FROM photo.runs WHERE tenant_id=x.tenant_id AND id=signer_run
  AND created_by=r.created_by FOR SHARE;
 IF sr.id IS NULL OR sr.kind<>'v2' OR sr.config_snapshot->>'workflow_type' IS DISTINCT FROM 'provenance'
  OR sr.config_snapshot->>'project_id' IS DISTINCT FROM d.project_id::text
  OR sr.config_snapshot->>'export_id' IS DISTINCT FROM i.export_id::text
  OR sr.config_snapshot->>'snapshot_id' IS DISTINCT FROM photo.v30_uuid5(signer_workflow,'provenance:artifact-snapshot:1')::text
  OR sw.input_snapshot_id IS DISTINCT FROM photo.v30_uuid5(signer_workflow,'provenance:artifact-snapshot:1')
 THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=x.tenant_id AND id=signer_task
  AND run_id=signer_run AND node_key='provenance.sign:0' AND operation='provenance.sign'
  AND operation_version='1' FOR SHARE;
 IF t.id IS NULL THEN
  RETURN QUERY SELECT 'pending'::text,i.export_id,signer_task;RETURN;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs policy_input
  WHERE policy_input.tenant_id=x.tenant_id AND policy_input.task_id=t.id
   AND policy_input.contract='SigningPolicy'
   AND (policy_input.artifact_id IS DISTINCT FROM photo.v30_uuid5(x.tenant_id,
    'signing:policy:'||(frozen->'policy'->>'signing_policy_id')||':'||(frozen->'policy'->>'signing_policy_sha'))
    OR policy_input.sha256 IS DISTINCT FROM frozen->'policy'->>'signing_policy_sha')) THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs policy_input
  WHERE policy_input.tenant_id=x.tenant_id AND policy_input.task_id=t.id
   AND policy_input.contract='SigningPolicy'
   AND policy_input.artifact_id=photo.v30_uuid5(x.tenant_id,
    'signing:policy:'||(frozen->'policy'->>'signing_policy_id')||':'||(frozen->'policy'->>'signing_policy_sha'))
   AND policy_input.sha256=frozen->'policy'->>'signing_policy_sha') THEN
  IF t.state='blocked' AND t.canonical_result_id IS NULL
   AND sr.status IN('draft','queued','running') THEN
   RETURN QUERY SELECT 'pending'::text,i.export_id,signer_task;
  END IF;
  RETURN;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs export_input
  WHERE export_input.tenant_id=x.tenant_id AND export_input.task_id=t.id
   AND export_input.contract='QualifiedExport'
   AND NOT EXISTS(SELECT 1 FROM photo.exports selected_export
    JOIN photo.artifacts original ON original.tenant_id=selected_export.tenant_id
     AND original.id=selected_export.artifact_id
    WHERE selected_export.tenant_id=x.tenant_id AND selected_export.id=i.export_id
     AND selected_export.status='ready' AND original.state='ready'
     AND original.verified_at IS NOT NULL AND export_input.artifact_id=original.id
     AND export_input.sha256=original.sha256)) THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs export_input
  JOIN photo.exports selected_export ON selected_export.tenant_id=export_input.tenant_id
   AND selected_export.id=i.export_id AND selected_export.status='ready'
  JOIN photo.artifacts original ON original.tenant_id=selected_export.tenant_id
   AND original.id=selected_export.artifact_id AND original.state='ready'
   AND original.verified_at IS NOT NULL
  WHERE export_input.tenant_id=x.tenant_id AND export_input.task_id=t.id
   AND export_input.contract='QualifiedExport' AND export_input.artifact_id=original.id
   AND export_input.sha256=original.sha256)
  AND (t.state<>'blocked' OR t.canonical_result_id IS NOT NULL) THEN RETURN;END IF;
 IF t.state IN('failed','dead') OR sr.status='failed' THEN
  RETURN QUERY SELECT 'failed'::text,i.export_id,signer_task;RETURN;
 END IF;
 IF t.state='cancelled' OR sr.status='cancelled' THEN
  RETURN QUERY SELECT 'cancelled'::text,i.export_id,signer_task;RETURN;
 END IF;
 IF t.state<>'succeeded' OR t.canonical_result_id IS NULL THEN
  RETURN QUERY SELECT 'pending'::text,i.export_id,signer_task;RETURN;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.provenance_manifests pm
  JOIN photo.artifacts signed ON signed.tenant_id=pm.tenant_id AND signed.id=pm.signed_artifact_id
  JOIN photo.artifacts manifest ON manifest.tenant_id=pm.tenant_id AND manifest.id=pm.manifest_artifact_id
  WHERE pm.tenant_id=x.tenant_id AND pm.project_id=d.project_id AND pm.export_id=i.export_id
   AND pm.workflow_id=signer_workflow AND pm.adopted_task_id=signer_task
   AND pm.adopted_result_id=t.canonical_result_id AND pm.validation_sha ~ '^[0-9a-f]{64}$'
   AND signed.state='ready' AND signed.verified_at IS NOT NULL
   AND manifest.state='ready' AND manifest.verified_at IS NOT NULL) THEN
  RETURN QUERY SELECT 'adopted'::text,i.export_id,signer_task;RETURN;
 END IF;
 RETURN QUERY SELECT 'succeeded_unverified'::text,i.export_id,signer_task;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.read_v30_delivery_signer_status(uuid,uuid,integer,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
