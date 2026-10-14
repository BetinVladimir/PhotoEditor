-- +goose Up
-- Lease-independent immutable-document authority for signed assembly. This
-- private reader is necessary after the parent expansion has been closed, but
-- does not by itself authorize worker dispatch or signer object adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_ready_document(p_tenant uuid,p_run uuid,p_owner uuid,
 p_purpose text,p_ref jsonb) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_purpose NOT IN('v30_delivery_signed_envelope','v30_delivery_assembly','v30_delivery_signed_closure')
  AND p_purpose !~ '^v30_delivery_slots_(00[0-9]|0[0-9][0-9]|1[0-5][0-9])$'
  OR jsonb_typeof(p_ref) IS DISTINCT FROM 'object' OR p_ref-'artifact_id'-'sha256'<>'{}'::jsonb
  OR coalesce(p_ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a
  ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose
  AND b.artifact_id=(p_ref->>'artifact_id')::uuid AND b.sha256=p_ref->>'sha256'
  AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
  AND b.bytes=octet_length(b.v30_delivery_document_jcs)
  AND encode(sha256(convert_to(b.v30_delivery_document_jcs,'UTF8')),'hex')=b.sha256
  AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket
  AND a.object_version=b.object_version AND a.object_version NOT IN('','null')
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||b.artifact_id
  AND a.media_type='application/json' AND a.output_role=b.purpose AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN d.v30_delivery_document_jcs;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_ready_document(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_ready_document(uuid,uuid,uuid,text,jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_runtime_documents(p_tenant uuid,p_run uuid,p_task uuid,
 p_params uuid,p_sha text,p_json text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;parent photo.tasks%ROWTYPE;prepare_doc photo.backend_documents%ROWTYPE;
 c photo.delivery_signing_closures%ROWTYPE;params_doc photo.backend_documents%ROWTYPE;
 frozen jsonb;params jsonb;envelope jsonb;assembly jsonb;closure jsonb;ref jsonb;
 envelope_raw text;assembly_raw text;closure_raw text;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536 OR p_sha IS NULL
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
  AND workflow_type='delivery' FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('running','waiting_review','completed')
  OR r.cancel_requested OR w.id IS NULL OR w.id<>photo.v30_uuid5(p_run,'workflow')
  OR w.renderer_version<>'v2.2' OR t.id IS NULL
  OR t.id<>photo.v30_uuid5(w.id,'v2.delivery.assemble:0')
  OR t.operation<>'v2.delivery.assemble' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
  OR t.state NOT IN('blocked','queued','running','succeeded') OR t.cancel_requested
  OR t.params_artifact_id IS DISTINCT FROM p_params THEN RETURN NULL;END IF;
 SELECT * INTO params_doc FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_task AND b.purpose='task_params'
  AND b.artifact_id=p_params AND b.sha256=p_sha AND b.bytes=octet_length(p_json)
  AND b.state='ready';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=params_doc.artifact_id AND a.sha256=params_doc.sha256 AND a.bytes=params_doc.bytes
  AND a.bucket=params_doc.bucket AND a.object_version=params_doc.object_version
  AND a.object_version NOT IN('','null') AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role='task_params'
  AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||a.id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 params:=p_json::jsonb;ref:=params->'assembly';
 IF params-'schema_version'-'assembly'<>'{}'::jsonb OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND workflow_id=w.id FOR SHARE;
 IF d.id IS NULL OR d.project_id<>w.project_id OR d.snapshot_id<>w.input_snapshot_id
  OR NOT ((r.status='running' AND d.state='building') OR
   (r.status='waiting_review' AND d.state='review') OR
   (r.status='completed' AND d.state='ready')) THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=photo.v30_uuid5(w.id,'v2.delivery.prepare:0') FOR SHARE;
 SELECT * INTO prepare_doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
  AND owner_id=parent.id AND purpose='task_params' AND artifact_id=parent.params_artifact_id
  AND state='ready';
 IF parent.id IS NULL OR parent.state<>'succeeded' OR parent.cancel_requested
  OR parent.canonical_result_id IS NULL OR prepare_doc.id IS NULL
  OR prepare_doc.v30_delivery_proof IS NULL OR NOT EXISTS(SELECT 1 FROM photo.inbox
   WHERE tenant_id=p_tenant AND consumer='v2-adopt'
    AND event_id=photo.v2_adoption_event_id(parent.id,parent.canonical_result_id)) THEN RETURN NULL;END IF;
 frozen:=(prepare_doc.v30_delivery_proof->>'frozen_input_jcs')::jsonb;
 IF frozen->'policy'->>'provenance' IS DISTINCT FROM 'required' THEN RETURN NULL;END IF;
 SELECT * INTO c FROM photo.delivery_signing_closures WHERE tenant_id=p_tenant
  AND project_id=d.project_id AND package_id=d.id AND parent_run_id=p_run
  AND actor_id=r.created_by FOR SHARE;
 IF NOT FOUND OR c.policy_id::text IS DISTINCT FROM frozen->'policy'->>'signing_policy_id'
  OR c.policy_sha IS DISTINCT FROM frozen->'policy'->>'signing_policy_sha'
  OR encode(sha256(convert_to(c.closure_jcs,'UTF8')),'hex') IS DISTINCT FROM c.closure_sha
  OR photo.v31_signing_policy_ready(p_tenant,c.policy_id,
   photo.v30_uuid5(p_tenant,'signing:policy:'||c.policy_id::text||':'||c.policy_sha),c.policy_sha)
   IS DISTINCT FROM true THEN RETURN NULL;END IF;
 envelope_raw:=photo.v30_delivery_signed_ready_document(p_tenant,p_run,p_task,
  'v30_delivery_signed_envelope',ref);
 IF envelope_raw IS NULL THEN RETURN NULL;END IF;
 envelope:=envelope_raw::jsonb;
 IF envelope-'schema_version'-'assembly'-'signing_closure'<>'{}'::jsonb
  OR envelope->'schema_version' IS DISTINCT FROM '2'::jsonb
  OR envelope->'assembly'->>'artifact_id'=envelope->'signing_closure'->>'artifact_id' THEN RETURN NULL;END IF;
 assembly_raw:=photo.v30_delivery_signed_ready_document(p_tenant,p_run,p_task,
  'v30_delivery_assembly',envelope->'assembly');
 closure_raw:=photo.v30_delivery_signed_ready_document(p_tenant,p_run,p_task,
  'v30_delivery_signed_closure',envelope->'signing_closure');
 IF assembly_raw IS NULL OR closure_raw IS NULL OR closure_raw IS DISTINCT FROM c.closure_jcs THEN RETURN NULL;END IF;
 assembly:=assembly_raw::jsonb;closure:=closure_raw::jsonb;
 IF assembly->'policy' IS DISTINCT FROM frozen->'policy'
  OR assembly->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR closure->>'tenant_id' IS DISTINCT FROM p_tenant::text
  OR closure->>'actor_id' IS DISTINCT FROM r.created_by::text
  OR closure->>'parent_id' IS DISTINCT FROM p_run::text
  OR closure->>'policy_id' IS DISTINCT FROM c.policy_id::text
  OR closure->>'policy_sha' IS DISTINCT FROM c.policy_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
  AND i.contract='SignedDeliveryAssemblyEnvelope')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id=(envelope->'assembly'->>'artifact_id')::uuid
   AND i.sha256=envelope->'assembly'->>'sha256' AND i.contract='DeliveryAssemblyInput')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id=(envelope->'signing_closure'->>'artifact_id')::uuid
   AND i.sha256=envelope->'signing_closure'->>'sha256'
   AND i.contract='SignedDeliveryAssemblyClosure') THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('envelope_jcs',envelope_raw,'assembly_jcs',assembly_raw,
  'closure_jcs',closure_raw,'proof',prepare_doc.v30_delivery_proof,
  'parent_task_id',parent.id,'project_id',d.project_id,'package_id',d.id,
  'actor_id',r.created_by);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_runtime_documents(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
