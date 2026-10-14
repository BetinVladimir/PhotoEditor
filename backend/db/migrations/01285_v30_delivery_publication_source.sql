-- +goose Up
-- Private source authority remains live after assembly commit for publication.
-- Worker gates still require their separate running run/task lease.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_assembly_source_context(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_sha text,p_json text) RETURNS jsonb
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
 IF r.id IS NULL OR r.kind<>'v2' OR r.status NOT IN('queued','running','waiting_review','completed') OR r.cancel_requested
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
 IF d.id IS NULL OR NOT ((r.status IN('queued','running') AND d.state='building') OR (r.status='waiting_review' AND d.state='review') OR (r.status='completed' AND d.state='ready')) OR d.project_id<>w.project_id OR d.snapshot_id<>w.input_snapshot_id THEN RETURN NULL;END IF;
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

CREATE OR REPLACE FUNCTION photo.v30_delivery_current_export_observation(p_tenant uuid,p_run uuid,p_package uuid,p_request jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;r photo.runs%ROWTYPE;x photo.exports%ROWTYPE;parent photo.runs%ROWTYPE;package photo.delivery_packages%ROWTYPE;workflow photo.v2_workflows%ROWTYPE;slot integer;
 a photo.artifacts%ROWTYPE;t photo.tasks%ROWTYPE;ready boolean;
BEGIN
 request:=p_request;
 IF jsonb_typeof(request) IS DISTINCT FROM 'object' OR request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR request->>'parent_run_id' IS DISTINCT FROM p_run::text OR coalesce(request->>'slot_index','') !~ '^(0|[1-9][0-9]{0,4})$' THEN RETURN NULL;END IF;
 slot:=(request->>'slot_index')::integer;
 IF slot>79999 OR request->>'child_run_id' IS DISTINCT FROM photo.v30_uuid5(p_run,'child:export:'||slot::text)::text THEN RETURN NULL;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 SELECT * INTO package FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=p_package AND workflow_id=workflow.id FOR SHARE;
 IF parent.id IS NULL OR parent.kind<>'v2' OR parent.cancel_requested OR parent.status NOT IN('queued','running','waiting_review','completed')
 OR workflow.id IS NULL OR workflow.workflow_type<>'delivery' OR package.id IS NULL OR NOT ((parent.status IN('queued','running') AND package.state='building') OR (parent.status='waiting_review' AND package.state='review') OR (parent.status='completed' AND package.state='ready')) THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM parent.created_by OR r.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows cw WHERE cw.tenant_id=p_tenant AND cw.run_id=r.id
 AND cw.project_id=workflow.project_id AND cw.input_snapshot_id=workflow.input_snapshot_id AND cw.workflow_type='delivery') THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'export_id')::uuid FOR SHARE;
 IF NOT FOUND OR x.asset_version_id::text IS DISTINCT FROM request->'binding'->>'asset_version_id'
 OR x.preset_hash IS DISTINCT FROM request->'preset'->>'preset_sha'
 OR x.id<>photo.v30_uuid5(r.id,'v30:delivery-export:1') THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.delivery_items i JOIN photo.delivery_packages d ON d.tenant_id=i.tenant_id AND d.id=i.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id AND w.run_id=p_run
 WHERE i.tenant_id=p_tenant AND i.export_id=x.id AND i.ordinal=(request->>'ordinal')::integer
 AND i.preset_id=(request->'preset'->>'preset_id')::uuid AND i.preset_sha=request->'preset'->>'preset_sha'
 AND d.id=p_package AND i.required IS NOT DISTINCT FROM (request->'preset'->>'required')::boolean AND d.state=package.state) THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=x.artifact_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=x.task_id;
 ready:=coalesce(x.status='ready' AND a.id IS NOT NULL AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.output_role='encoded_image'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND t.operation='export.encode' AND t.state='succeeded' AND NOT t.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)),false);
 RETURN jsonb_build_object('export_id',x.id,'snapshot_digest',request->>'snapshot_digest','binding',request->'binding',
 'preset_sha',request->'preset'->>'preset_sha','run_state',r.status,'export_state',x.status,'artifact_ready',ready,
 'artifact',CASE WHEN ready THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
 'reason',CASE WHEN r.status='cancelled' THEN 'EXPORT_CANCELLED' WHEN r.status='failed' THEN 'EXPORT_FAILED' ELSE '' END);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_current_export_observation(uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_api,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_delivery_publication_authority(p_tenant uuid,p_package uuid,p_actor uuid,p_session uuid,p_revision bigint) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.delivery_packages%ROWTYPE;w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;a photo.artifacts%ROWTYPE;
 root_ref jsonb;params text;previous text;ready boolean;
BEGIN
 IF p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740991 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=p_package FOR UPDATE;
 IF NOT FOUND OR d.revision<>p_revision OR d.state NOT IN('review','ready') OR d.published_at IS NOT NULL
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=d.workflow_id;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id AND id=photo.v30_uuid5(w.id,'v2.delivery.assemble:0') FOR SHARE;
 IF NOT FOUND OR photo.v30_delivery_assembly_adoption_committed(p_tenant,t.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=d.manifest_artifact_id FOR SHARE;
 IF NOT FOUND OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO root_ref FROM photo.v2_task_inputs
 WHERE tenant_id=p_tenant AND task_id=t.id AND contract='DeliveryAssemblyInput';
 IF root_ref IS NULL THEN RETURN NULL;END IF;
 params:=format('{"assembly":{"artifact_id":"%s","sha256":"%s"},"schema_version":1}',root_ref->>'artifact_id',root_ref->>'sha256');
 previous:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',p_tenant::text,true);
 BEGIN
  ready:=photo.v30_delivery_assembly_source_ready(p_tenant,t.run_id,t.id,t.params_artifact_id,encode(sha256(convert_to(params,'UTF8')),'hex'),params);
 EXCEPTION WHEN OTHERS THEN PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RAISE;END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
 IF ready IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run_id',t.run_id,'task_id',t.id,'manifest_artifact_id',a.id,'manifest_sha',a.sha256,'state',d.state,'project_id',d.project_id);
END $$;
ALTER FUNCTION photo.v30_delivery_publication_authority(uuid,uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_publication_authority(uuid,uuid,uuid,uuid,bigint) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
