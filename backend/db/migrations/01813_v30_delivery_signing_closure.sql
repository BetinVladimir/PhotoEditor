-- +goose Up
-- Retained V31 dependency evidence for a V30 parent. This migration does not
-- activate provenance=required or replace assembly/publication admission.
CREATE TABLE photo.delivery_signing_closures (
 tenant_id uuid NOT NULL,
 project_id uuid NOT NULL,
 package_id uuid NOT NULL,
 parent_run_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 policy_id uuid NOT NULL,
 policy_sha text NOT NULL CHECK (policy_sha ~ '^[0-9a-f]{64}$'),
 closure_sha text NOT NULL CHECK (closure_sha ~ '^[0-9a-f]{64}$'),
 closure_jcs text NOT NULL CHECK (octet_length(closure_jcs) BETWEEN 2 AND 8388608),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,package_id),
 FOREIGN KEY(tenant_id,project_id,package_id)
  REFERENCES photo.delivery_packages(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.delivery_signing_closures ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.delivery_signing_closures FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.delivery_signing_closures TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.delivery_signing_closures FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT SELECT,INSERT,DELETE ON photo.delivery_signing_closures TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.retain_v30_delivery_signing_closure(p_expansion uuid,p_token uuid,p_sha text,p_jcs text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;d photo.delivery_packages%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 body jsonb;frozen jsonb;entry jsonb;ordinal integer;preset uuid;export_id uuid;signing_key text;signing_workflow uuid;
 hint_tenant uuid;hint_run uuid;
 v_count integer;seen_count integer:=0;previous_ordinal integer:=-1;previous_preset text:='';prior photo.delivery_signing_closures%ROWTYPE;
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_jcs IS NULL
  OR octet_length(p_jcs) NOT BETWEEN 2 AND 8388608
  OR encode(sha256(convert_to(p_jcs,'UTF8')),'hex')<>p_sha THEN RETURN false;END IF;
 SELECT tenant_id,run_id INTO hint_tenant,hint_run FROM photo.graph_expansions
  WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=hint_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=hint_tenant AND id=hint_run AND status='running'
  AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND tenant_id=hint_tenant
  AND run_id=hint_run AND kind='v2.delivery/1' AND state='open'
  AND expansion_token=p_token AND expansion_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND workflow_type='delivery' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=x.tenant_id AND workflow_id=w.id
  AND state IN('building','review') FOR UPDATE;
 IF NOT FOUND OR d.project_id<>w.project_id THEN RETURN false;END IF;
 body:=p_jcs::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR
  (SELECT count(*) FROM jsonb_object_keys(body))<>7
  OR body->>'schema_version' IS DISTINCT FROM '1' OR body->>'tenant_id' IS DISTINCT FROM x.tenant_id::text
  OR body->>'parent_id' IS DISTINCT FROM x.run_id::text
  OR body->>'policy_sha' IS NULL OR body->>'policy_sha' !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(body->'entries') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF (body->>'actor_id') IS NULL OR (body->>'policy_id') IS NULL
  OR (body->>'actor_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
  OR (body->>'policy_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_id' IS DISTINCT FROM body->>'policy_id'
  OR frozen->'policy'->>'signing_policy_sha' IS DISTINCT FROM body->>'policy_sha'
  OR (SELECT r.created_by::text FROM photo.runs r WHERE r.tenant_id=x.tenant_id AND r.id=x.run_id)
   IS DISTINCT FROM body->>'actor_id' THEN RETURN false;END IF;
 IF photo.v31_signing_policy_ready(x.tenant_id,(body->>'policy_id')::uuid,
   photo.v30_uuid5(x.tenant_id,'signing:policy:'||(body->>'policy_id')||':'||(body->>'policy_sha')),
   body->>'policy_sha') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT count(*) INTO v_count FROM photo.delivery_items i WHERE i.tenant_id=x.tenant_id
  AND i.package_id=d.id AND i.state='ready';
 IF v_count<1 OR v_count<>jsonb_array_length(body->'entries') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.delivery_items i WHERE i.tenant_id=x.tenant_id AND i.package_id=d.id
  AND i.state NOT IN('ready','omitted')) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(body->'entries') LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR
   (SELECT count(*) FROM jsonb_object_keys(entry))<>10 THEN RETURN false;END IF;
  ordinal:=(entry->>'ordinal')::integer;preset:=(entry->>'preset_id')::uuid;
  export_id:=(entry->>'export_id')::uuid;signing_key:=entry->>'signing_key';
  IF ordinal<0 OR ordinal>4999 OR (ordinal<previous_ordinal OR
   (ordinal=previous_ordinal AND preset::text<=previous_preset)) THEN RETURN false;END IF;
  previous_ordinal:=ordinal;previous_preset:=preset::text;
  IF signing_key IS DISTINCT FROM photo.v30_uuid5(x.run_id,ordinal::text||':'||preset::text)::text THEN RETURN false;END IF;
  signing_workflow:=photo.v30_uuid5(x.tenant_id,'provenance:'||(body->>'actor_id')||':'||signing_key);
  IF entry->>'task_id' IS DISTINCT FROM photo.v30_uuid5(signing_workflow,'provenance.sign:0')::text
   OR NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.exports e
    ON e.tenant_id=i.tenant_id AND e.id=i.export_id AND e.status='ready'
    JOIN photo.artifacts original ON original.tenant_id=e.tenant_id AND original.id=e.artifact_id
    JOIN photo.provenance_manifests pm ON pm.tenant_id=e.tenant_id AND pm.export_id=e.id
     AND pm.project_id=d.project_id AND pm.signing_policy_sha=body->>'policy_sha'
    JOIN photo.v2_workflows sw ON sw.tenant_id=pm.tenant_id AND sw.id=pm.workflow_id AND sw.id=signing_workflow
     AND sw.workflow_type='provenance' AND sw.renderer_version='v2.2'
     AND sw.project_id=d.project_id AND sw.policy_sha=body->>'policy_sha'
     AND sw.run_id=photo.v30_uuid5(signing_workflow,'run')
     AND sw.input_snapshot_id=photo.v30_uuid5(signing_workflow,'provenance:artifact-snapshot:1')
    JOIN photo.runs signer_run ON signer_run.tenant_id=sw.tenant_id AND signer_run.id=sw.run_id
     AND signer_run.kind='v2' AND signer_run.graph_sealed AND NOT signer_run.cancel_requested
     AND signer_run.input_snapshot_id=sw.input_snapshot_id
     AND signer_run.created_by=(body->>'actor_id')::uuid
     AND signer_run.config_snapshot->>'workflow_type'='provenance'
     AND signer_run.config_snapshot->>'project_id'=d.project_id::text
     AND signer_run.config_snapshot->>'export_id'=i.export_id::text
     AND signer_run.config_snapshot->>'snapshot_id'=sw.input_snapshot_id::text
    JOIN photo.tasks task ON task.tenant_id=pm.tenant_id AND task.id=pm.adopted_task_id
     AND task.run_id=sw.run_id AND task.node_key='provenance.sign:0'
     AND task.state='succeeded' AND task.operation='provenance.sign'
     AND task.operation_version='1' AND task.canonical_result_id=pm.adopted_result_id
    JOIN photo.v2_task_inputs policy_input ON policy_input.tenant_id=task.tenant_id
     AND policy_input.task_id=task.id AND policy_input.contract='SigningPolicy'
     AND policy_input.artifact_id=photo.v30_uuid5(x.tenant_id,'signing:policy:'||(body->>'policy_id')||':'||(body->>'policy_sha'))
     AND policy_input.sha256=body->>'policy_sha'
    JOIN photo.v2_task_inputs export_input ON export_input.tenant_id=task.tenant_id
     AND export_input.task_id=task.id AND export_input.contract='QualifiedExport'
     AND export_input.artifact_id=original.id AND export_input.sha256=original.sha256
    JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=pm.adopted_result_id
     AND result.task_id=task.id
    JOIN photo.artifacts signed ON signed.tenant_id=pm.tenant_id AND signed.id=pm.signed_artifact_id
    JOIN photo.artifacts manifest ON manifest.tenant_id=pm.tenant_id AND manifest.id=pm.manifest_artifact_id
    WHERE i.tenant_id=x.tenant_id AND i.package_id=d.id AND i.ordinal=ordinal AND i.preset_id=preset
     AND i.export_id=export_id AND i.state='ready'
     AND original.id=(entry->'export'->>'artifact_id')::uuid
     AND original.sha256=entry->'export'->>'sha256' AND original.state='ready'
     AND original.verified_at IS NOT NULL AND original.media_type IN('image/jpeg','image/png')
     AND coalesce(original.object_version,'') NOT IN('','null')
     AND pm.adopted_task_id=(entry->>'task_id')::uuid AND pm.adopted_result_id=(entry->>'result_id')::uuid
     AND pm.validation_sha=entry->>'validation_sha' AND pm.validation_sha ~ '^[0-9a-f]{64}$'
     AND signed.id=(entry->'signed'->>'artifact_id')::uuid AND signed.sha256=entry->'signed'->>'sha256'
     AND signed.state='ready' AND signed.verified_at IS NOT NULL
     AND signed.created_by_task_id=task.id AND signed.produced_attempt=result.attempt
     AND signed.produced_fence=result.fencing_token AND signed.output_role='signed_export'
     AND signed.media_type=original.media_type AND signed.bucket=original.bucket
     AND signed.object_key='tenants/'||x.tenant_id||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||signed.id
     AND coalesce(signed.object_version,'') NOT IN('','null')
     AND manifest.id=(entry->'manifest'->>'artifact_id')::uuid AND manifest.sha256=entry->'manifest'->>'sha256'
     AND manifest.state='ready' AND manifest.verified_at IS NOT NULL
     AND manifest.created_by_task_id=task.id AND manifest.produced_attempt=result.attempt
     AND manifest.produced_fence=result.fencing_token AND manifest.output_role='manifest'
     AND manifest.media_type='application/c2pa' AND manifest.bucket=original.bucket
     AND manifest.object_key='tenants/'||x.tenant_id||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||manifest.id
     AND coalesce(manifest.object_version,'') NOT IN('','null'))
  THEN RETURN false;END IF;
  seen_count:=seen_count+1;
 END LOOP;
 IF seen_count<>v_count OR NOT EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.id=x.id
  AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()) THEN RETURN false;END IF;
 SELECT * INTO prior FROM photo.delivery_signing_closures WHERE tenant_id=x.tenant_id AND package_id=d.id FOR UPDATE;
 IF FOUND THEN RETURN prior.closure_sha=p_sha AND prior.closure_jcs=p_jcs
  AND prior.actor_id=(body->>'actor_id')::uuid AND prior.policy_id=(body->>'policy_id')::uuid;END IF;
 INSERT INTO photo.delivery_signing_closures(tenant_id,project_id,package_id,parent_run_id,actor_id,policy_id,policy_sha,closure_sha,closure_jcs)
 VALUES(x.tenant_id,d.project_id,d.id,x.run_id,(body->>'actor_id')::uuid,(body->>'policy_id')::uuid,body->>'policy_sha',p_sha,p_jcs);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 OR check_violation OR foreign_key_violation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.retain_v30_delivery_signing_closure(uuid,uuid,text,text) TO photo_scheduler;

-- Tenant purge must delete the child before delivery_packages. This explicit
-- allowlist extension uses the previously authorized photo_fn_owner DELETE.
-- +goose StatementBegin
DO $$ DECLARE definition text;old_value text:='''delivery_items'',''delivery_packages''';new_value text:='''delivery_signing_closures'',''delivery_items'',''delivery_packages''';
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(old_value IN definition)=0 THEN RAISE EXCEPTION 'TENANT_PURGE_SIGNING_CLOSURE_SHAPE';END IF;
 EXECUTE replace(definition,old_value,new_value);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
