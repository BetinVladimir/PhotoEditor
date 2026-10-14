-- +goose Up
-- Exact package/slot file authority for a future short-lived user intent.
-- This reader exposes storage coordinates only to photo_api after current
-- session, project and publication checks. It does not create an intent.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_download_target(
 p_tenant uuid,p_actor uuid,p_session uuid,p_package uuid,p_kind text,
 p_ordinal integer,p_preset uuid,p_bucket text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE resource jsonb;d photo.delivery_packages%ROWTYPE;r photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;prepare_task photo.tasks%ROWTYPE;
 prepare_doc photo.backend_documents%ROWTYPE;item photo.delivery_items%ROWTYPE;
 export_row photo.exports%ROWTYPE;closure photo.delivery_signing_closures%ROWTYPE;
 candidate photo.artifacts%ROWTYPE;policy jsonb;frozen jsonb;entry jsonb;entry_count integer;
 preset_count integer;preset_position integer;slot_index integer;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_session IS NULL OR p_package IS NULL
  OR p_kind IS NULL OR p_kind NOT IN('manifest','slot') OR p_bucket IS NULL
  OR length(p_bucket) NOT BETWEEN 3 AND 63
  OR (p_kind='manifest' AND (p_ordinal IS NOT NULL OR p_preset IS NOT NULL))
  OR (p_kind='slot' AND (p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_preset IS NULL))
 THEN RETURN;END IF;
 resource:=photo.get_v30_delivery(p_tenant,p_actor,p_session,p_package);
 IF resource IS NULL OR resource->'document' IS NULL
  OR resource->'document'='null'::jsonb THEN RETURN;END IF;
 SELECT package.* INTO d FROM photo.delivery_packages package
 JOIN photo.projects project ON project.tenant_id=package.tenant_id
  AND project.id=package.project_id AND project.archived_at IS NULL
  AND project.download_allowed
 WHERE package.tenant_id=p_tenant AND package.id=p_package
  AND package.state='ready' AND package.published_at IS NOT NULL FOR SHARE OF package;
 IF d.id IS NULL OR resource->'document'->>'artifact_id' IS DISTINCT FROM
  d.manifest_artifact_id::text THEN RETURN;END IF;
 SELECT run.* INTO r FROM photo.runs run JOIN photo.v2_workflows workflow
  ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
  AND workflow.id=d.workflow_id AND workflow.workflow_type='delivery'
 WHERE run.tenant_id=p_tenant AND run.status='completed'
  AND run.graph_sealed AND NOT run.cancel_requested FOR SHARE OF run;
 IF r.id IS NULL THEN RETURN;END IF;
 SELECT workflow.* INTO w FROM photo.v2_workflows workflow
 WHERE workflow.tenant_id=p_tenant AND workflow.id=d.workflow_id
  AND workflow.run_id=r.id FOR SHARE;
 SELECT task.* INTO prepare_task FROM photo.tasks task
 WHERE task.tenant_id=p_tenant AND task.run_id=r.id
  AND task.id=photo.v30_uuid5(w.id,'v2.delivery.prepare:0')
  AND task.state='succeeded' AND NOT task.cancel_requested FOR SHARE;
 SELECT doc.* INTO prepare_doc FROM photo.backend_documents doc
 WHERE doc.tenant_id=p_tenant AND doc.run_id=r.id
  AND doc.owner_id=prepare_task.id AND doc.purpose='task_params'
  AND doc.artifact_id=prepare_task.params_artifact_id AND doc.state='ready'
  AND doc.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF prepare_doc.id IS NULL THEN RETURN;END IF;
 frozen:=(prepare_doc.v30_delivery_proof->>'frozen_input_jcs')::jsonb;
 policy:=frozen->'policy';
 IF policy->>'provenance' IS NULL OR policy->>'provenance' NOT IN('off','required') THEN RETURN;END IF;
 -- A revoked signing policy blocks the package manifest as well as its slots.
 IF policy->>'provenance'='required' AND (
  policy->>'signing_policy_id' IS NULL OR
  policy->>'signing_policy_sha' !~ '^[0-9a-f]{64}$' OR
  photo.v31_signing_policy_ready(p_tenant,(policy->>'signing_policy_id')::uuid,
   photo.v30_uuid5(p_tenant,'signing:policy:'||
    (policy->>'signing_policy_id')||':'||(policy->>'signing_policy_sha')),
   policy->>'signing_policy_sha') IS DISTINCT FROM true
 ) THEN RETURN;END IF;
 IF policy->>'provenance'='required' THEN
  SELECT c.* INTO closure FROM photo.delivery_signing_closures c
  WHERE c.tenant_id=p_tenant AND c.project_id=d.project_id
   AND c.package_id=d.id AND c.parent_run_id=r.id AND c.actor_id=r.created_by
   AND c.policy_id::text=policy->>'signing_policy_id'
   AND c.policy_sha=policy->>'signing_policy_sha' FOR SHARE;
  IF closure.package_id IS NULL OR closure.closure_jcs IS NULL
   OR encode(sha256(convert_to(closure.closure_jcs,'UTF8')),'hex')<>closure.closure_sha
   OR closure.closure_jcs::jsonb->>'tenant_id' IS DISTINCT FROM p_tenant::text
   OR closure.closure_jcs::jsonb->>'actor_id' IS DISTINCT FROM r.created_by::text
   OR closure.closure_jcs::jsonb->>'parent_id' IS DISTINCT FROM r.id::text
   OR closure.closure_jcs::jsonb->>'policy_id' IS DISTINCT FROM closure.policy_id::text
   OR closure.closure_jcs::jsonb->>'policy_sha' IS DISTINCT FROM closure.policy_sha
  THEN RETURN;END IF;
 END IF;
 IF p_kind='manifest' THEN
  SELECT a.* INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=d.manifest_artifact_id AND a.sha256=resource->'document'->>'sha256'
   AND a.media_type='application/json' AND a.output_role='document' FOR SHARE;
 ELSE
  SELECT i.* INTO item FROM photo.delivery_items i WHERE i.tenant_id=p_tenant
   AND i.package_id=d.id AND i.ordinal=p_ordinal AND i.preset_id=p_preset
   AND i.state='ready' AND i.export_id IS NOT NULL FOR SHARE;
  IF item.id IS NULL THEN RETURN;END IF;
  SELECT e.* INTO export_row FROM photo.exports e WHERE e.tenant_id=p_tenant
   AND e.id=item.export_id AND e.status='ready' AND e.artifact_id IS NOT NULL FOR SHARE;
  preset_count:=jsonb_array_length(frozen->'presets');
  SELECT count(*) INTO preset_position FROM jsonb_array_elements(frozen->'presets') preset
   WHERE preset->>'preset_id'<p_preset::text;
  slot_index:=p_ordinal*preset_count+preset_position;
  IF export_row.id IS NULL OR export_row.preset_hash IS DISTINCT FROM item.preset_sha
   OR preset_count NOT BETWEEN 1 AND 16 OR slot_index NOT BETWEEN 0 AND 79999
   OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(frozen->'presets') preset
    WHERE preset->>'preset_id'=p_preset::text
     AND preset->>'preset_sha'=item.preset_sha)
   OR NOT EXISTS(
    SELECT 1 FROM photo.tasks export_task
    JOIN photo.runs export_run ON export_run.tenant_id=export_task.tenant_id
     AND export_run.id=export_task.run_id AND export_run.kind='v2'
     AND export_run.graph_sealed
     AND NOT export_run.cancel_requested AND export_run.created_by=r.created_by
     AND export_run.input_snapshot_id=d.snapshot_id
     AND export_run.id=photo.v30_uuid5(r.id,'child:export:'||slot_index::text)
     AND export_run.config_snapshot->>'export_id'=export_row.id::text
     AND export_run.config_snapshot->'v30_delivery_export'->>'parent_run_id'=r.id::text
     AND export_run.config_snapshot->'v30_delivery_export'->>'child_run_id'=export_run.id::text
     AND export_run.config_snapshot->'v30_delivery_export'->>'slot_index'=slot_index::text
     AND export_run.config_snapshot->'v30_delivery_export'->>'ordinal'=p_ordinal::text
     AND export_run.config_snapshot->'v30_delivery_export'->>'snapshot_digest'=frozen->>'snapshot_digest'
     AND export_run.config_snapshot->'v30_delivery_export'->'preset'->>'preset_id'=p_preset::text
     AND export_run.config_snapshot->'v30_delivery_export'->'preset'->>'preset_sha'=item.preset_sha
     AND export_run.config_snapshot->'v30_delivery_export'->'preset'->>'required'=item.required::text
     AND export_run.config_snapshot->'v30_delivery_export'->'binding'->>'asset_version_id'=export_row.asset_version_id::text
    JOIN photo.v2_workflows export_workflow
     ON export_workflow.tenant_id=export_run.tenant_id
     AND export_workflow.run_id=export_run.id
     AND export_workflow.project_id=d.project_id
     AND export_workflow.input_snapshot_id=d.snapshot_id
     AND export_workflow.workflow_type='delivery'
    JOIN photo.task_results export_result
     ON export_result.tenant_id=export_task.tenant_id
     AND export_result.id=export_task.canonical_result_id
     AND export_result.task_id=export_task.id
     AND export_result.attempt=export_task.attempt
     AND export_result.fencing_token=export_task.fencing_token
    JOIN photo.artifacts encoded ON encoded.tenant_id=export_task.tenant_id
     AND encoded.id=export_row.artifact_id AND encoded.state='ready'
     AND encoded.verified_at IS NOT NULL
     AND encoded.output_role='encoded_image'
     AND encoded.created_by_task_id=export_task.id
     AND encoded.produced_attempt=export_result.attempt
     AND encoded.produced_fence=export_result.fencing_token
    WHERE export_task.tenant_id=p_tenant AND export_task.id=export_row.task_id
     AND export_task.operation='export.encode' AND export_task.state='succeeded'
     AND export_row.id=photo.v30_uuid5(export_run.id,'v30:delivery-export:1')
     AND NOT export_task.cancel_requested
     AND EXISTS(SELECT 1 FROM photo.inbox adopted
      WHERE adopted.tenant_id=p_tenant AND adopted.consumer='v2-adopt'
       AND adopted.event_id=photo.v2_adoption_event_id(
        export_task.id,export_result.id))
   ) THEN RETURN;END IF;
  IF policy->>'provenance'='off' THEN
   SELECT a.* INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=export_row.artifact_id AND a.output_role='encoded_image'
    AND a.media_type IN('image/jpeg','image/png','image/tiff','image/webp','image/avif') FOR SHARE;
  ELSE
   SELECT count(*),min(value::text)::jsonb INTO entry_count,entry
   FROM jsonb_array_elements(closure.closure_jcs::jsonb->'entries') value
   WHERE (value->>'ordinal')::integer=p_ordinal
    AND value->>'preset_id'=p_preset::text;
   IF entry_count<>1 OR entry->>'export_id' IS DISTINCT FROM item.export_id::text
    OR entry->'export'->>'artifact_id' IS DISTINCT FROM export_row.artifact_id::text
    OR NOT EXISTS(SELECT 1 FROM photo.artifacts original
     WHERE original.tenant_id=p_tenant AND original.id=export_row.artifact_id
      AND original.sha256=entry->'export'->>'sha256' AND original.state='ready'
      AND original.verified_at IS NOT NULL AND original.object_version NOT IN('','null')
      AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp()))
   THEN RETURN;END IF;
   SELECT a.* INTO candidate FROM photo.artifacts a
   JOIN photo.provenance_manifests pm ON pm.tenant_id=a.tenant_id
    AND pm.signed_artifact_id=a.id AND pm.export_id=item.export_id
    AND pm.project_id=d.project_id AND pm.signing_policy_sha=closure.policy_sha
    AND pm.adopted_task_id=(entry->>'task_id')::uuid
    AND pm.adopted_result_id=(entry->>'result_id')::uuid
    AND pm.validation_sha=entry->>'validation_sha'
   JOIN photo.tasks signer_task ON signer_task.tenant_id=pm.tenant_id
    AND signer_task.id=pm.adopted_task_id AND signer_task.state='succeeded'
    AND NOT signer_task.cancel_requested AND signer_task.operation='provenance.sign'
    AND signer_task.operation_version='1'
    AND signer_task.canonical_result_id=pm.adopted_result_id
   JOIN photo.task_results signer_result ON signer_result.tenant_id=signer_task.tenant_id
    AND signer_result.id=pm.adopted_result_id AND signer_result.task_id=signer_task.id
   JOIN photo.v2_workflows signer_workflow ON signer_workflow.tenant_id=pm.tenant_id
    AND signer_workflow.id=pm.workflow_id AND signer_workflow.run_id=signer_task.run_id
    AND signer_workflow.workflow_type='provenance' AND signer_workflow.project_id=d.project_id
    AND signer_workflow.policy_sha=closure.policy_sha
    AND signer_workflow.id=photo.v30_uuid5(p_tenant,
     'provenance:'||r.created_by::text||':'||(entry->>'signing_key'))
   JOIN photo.runs signer_run ON signer_run.tenant_id=signer_workflow.tenant_id
    AND signer_run.id=signer_workflow.run_id AND signer_run.graph_sealed
    AND NOT signer_run.cancel_requested AND signer_run.created_by=r.created_by
   JOIN photo.artifacts original ON original.tenant_id=export_row.tenant_id
    AND original.id=export_row.artifact_id AND original.sha256=entry->'export'->>'sha256'
   WHERE a.tenant_id=p_tenant AND a.id=(entry->'signed'->>'artifact_id')::uuid
    AND a.sha256=entry->'signed'->>'sha256'
    AND a.output_role='signed_export' AND a.media_type=original.media_type
    AND a.media_type IN('image/jpeg','image/png')
    AND a.created_by_task_id=signer_task.id
    AND a.produced_attempt=signer_result.attempt
    AND a.produced_fence=signer_result.fencing_token
    AND a.object_key='tenants/'||p_tenant||'/tasks/'||signer_task.id||
     '/attempts/'||signer_result.attempt||'/fences/'||
     signer_result.fencing_token||'/'||a.id FOR SHARE OF a;
  END IF;
 END IF;
 IF candidate.id IS NULL OR candidate.bucket IS DISTINCT FROM p_bucket
  OR candidate.state<>'ready' OR candidate.verified_at IS NULL
  OR candidate.object_key IS NULL OR candidate.object_version IS NULL
  OR candidate.object_version IN('','null') OR candidate.bytes NOT BETWEEN 1 AND 536870912
  OR (candidate.expires_at IS NOT NULL AND candidate.expires_at<=clock_timestamp())
 THEN RETURN;END IF;
 RETURN QUERY SELECT candidate.id,candidate.sha256,candidate.bucket,
  candidate.object_key,candidate.object_version,candidate.bytes,candidate.media_type;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_download_target(uuid,uuid,uuid,uuid,text,integer,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_download_target(uuid,uuid,uuid,uuid,text,integer,uuid,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_download_target(uuid,uuid,uuid,uuid,text,integer,uuid,text)
 TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
