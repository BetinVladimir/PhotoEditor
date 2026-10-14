-- +goose Up
-- Lease-independent source context for activation/runtime matrix validation.
-- Private: not a worker source-ready gate and never sufficient by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_assembly_source_context(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;parent photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;prepare_doc photo.backend_documents%ROWTYPE;root_doc photo.backend_documents%ROWTYPE;
 params jsonb;ref jsonb;input_ref jsonb;prepare_params text;
BEGIN
 IF p_json IS NULL OR octet_length(p_json) NOT BETWEEN 2 AND 65536 OR p_sha IS NULL
 OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running') OR r.cancel_requested
 OR w.id IS NULL OR w.id<>photo.v30_uuid5(p_run,'workflow') OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2' THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.id<>photo.v30_uuid5(w.id,'v2.delivery.assemble:0') OR t.operation<>'v2.delivery.assemble'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.cancel_requested OR t.params_artifact_id IS DISTINCT FROM p_params THEN RETURN NULL;END IF;
 params:=p_json::jsonb;ref:=params->'assembly';
 IF params-'schema_version'-'assembly'<>'{}'::jsonb OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
 OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task
 AND b.purpose='task_params' AND b.artifact_id=p_params AND b.sha256=p_sha AND b.bytes=octet_length(p_json) AND b.state='ready') THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND workflow_id=w.id FOR SHARE;
 IF d.id IS NULL OR d.state<>'building' OR d.project_id<>w.project_id OR d.snapshot_id<>w.input_snapshot_id THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.v30_uuid5(w.id,'v2.delivery.prepare:0') FOR SHARE;
 IF parent.id IS NULL OR parent.state<>'succeeded' OR parent.cancel_requested OR parent.canonical_result_id IS NULL
 OR parent.operation<>'v2.delivery.prepare' OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(parent.id,parent.canonical_result_id)) THEN RETURN NULL;END IF;
 SELECT * INTO prepare_doc FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=parent.id
 AND purpose='task_params' AND artifact_id=parent.params_artifact_id AND state='ready';
 IF prepare_doc.id IS NULL OR prepare_doc.v30_delivery_proof IS NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO input_ref FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=parent.id AND contract='FrozenDeliveryInput';
 IF input_ref IS NULL THEN RETURN NULL;END IF;
 prepare_params:=jsonb_build_object('schema_version',1,'input',input_ref)::text;
 IF photo.v30_delivery_prepare_source_ready(p_tenant,p_run,parent.id,parent.params_artifact_id,prepare_doc.sha256,prepare_params) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT b.* INTO root_doc FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task AND b.purpose='v30_delivery_assembly'
 AND b.artifact_id=(ref->>'artifact_id')::uuid AND b.sha256=ref->>'sha256' AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.output_role=b.purpose
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||b.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF root_doc.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
 AND artifact_id=root_doc.artifact_id AND sha256=root_doc.sha256 AND contract='DeliveryAssemblyInput')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links WHERE tenant_id=p_tenant AND workflow_id=w.id AND artifact_id=root_doc.artifact_id AND sha256=root_doc.sha256) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('assembly_jcs',root_doc.v30_delivery_document_jcs,'proof',prepare_doc.v30_delivery_proof,
 'parent_task_id',parent.id,'project_id',w.project_id,'package_id',d.id);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_source_context(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_source_context(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
