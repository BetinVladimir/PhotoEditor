-- +goose Up
-- Completed native preparation replay rechecks live original evidence and exact sealed closure.
-- Native defect preparation replay gate; caller retains all source, policy and input authority locks.
-- Never activates a task or repairs partial state.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_completed_registration_state(p_tenant uuid,p_run uuid,p_task uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb,p_rows jsonb,p_manifest jsonb,p_chunks jsonb,p_direct jsonb,p_count integer,p_entries_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE; actual jsonb; transport jsonb; expected_transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'conflict';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed
 OR t.operation<>'defects.propose' OR t.operation_version<>'1'
 OR jsonb_typeof(r.config_snapshot->'v27_defect_prepare') IS DISTINCT FROM 'object'
 OR r.config_snapshot->'v27_defect_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR r.config_snapshot->'v27_defect_prepare'->>'task_id' IS DISTINCT FROM p_task::text
 OR r.config_snapshot->'v27_defect_prepare'->>'input_digest' IS DISTINCT FROM p_digest
 OR r.config_snapshot->'v27_defect_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_direct) IS DISTINCT FROM 'array' OR jsonb_array_length(p_direct)<>3
 OR NOT (p_direct @> jsonb_build_array(jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)))
 OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_params_sha AND state='ready')
 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows)<>p_count
 OR p_count NOT BETWEEN 3 AND 4097 OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal),'[]') INTO actual
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal<1000000;
 IF NOT (t.execution_profile ? 'v2_input_manifest') THEN RETURN 'conflict';END IF;
 IF actual IS DISTINCT FROM p_rows OR t.inputs IS DISTINCT FROM jsonb_build_array(p_manifest)||p_direct
 OR t.execution_profile->'v2_input_manifest' IS DISTINCT FROM p_manifest
 OR t.execution_profile->>'v2_entry_count' IS DISTINCT FROM p_count::text
 OR t.execution_profile->>'v2_entries_sha' IS DISTINCT FROM p_entries_sha
 OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM p_profile
 THEN RETURN 'conflict';END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal),'[]') INTO transport
 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000;
 expected_transport:=p_chunks||jsonb_build_array(p_manifest);
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=1000000
 AND (i.ordinal NOT BETWEEN 1000000 AND 1000000+jsonb_array_length(p_chunks)
 OR i.contract IS DISTINCT FROM CASE WHEN i.ordinal=1000000+jsonb_array_length(p_chunks) THEN 'InputClosureManifest' ELSE 'InputClosureChunk' END)) THEN RETURN 'conflict';END IF;
 IF transport IS DISTINCT FROM expected_transport OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN 'conflict';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(expected_transport) WITH ORDINALITY ref(value,n)
 WHERE NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_task
 AND b.state='ready' AND b.artifact_id=(ref.value->>'artifact_id')::uuid AND b.sha256=ref.value->>'sha256'
 AND b.purpose=CASE WHEN ref.n=jsonb_array_length(expected_transport) THEN 'v2_input_manifest' ELSE 'v2_input_chunk_'||(ref.n-1)::text END)) THEN RETURN 'conflict';END IF;
 RETURN 'replayed';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_defect_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) TO photo_scheduler;
-- Exact registered transport packet only; no queue activation or model authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_completed_binding_state(p_tenant uuid,p_run uuid,p_binding jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE transport jsonb;params jsonb;count_entries integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_binding) IS DISTINCT FROM 'object' OR octet_length(p_binding::text)>4194304
 OR p_binding->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_binding-'schema_version'-'task_id'-'params'-'input_digest'-'profile'-'transport')<>'{}'
 OR jsonb_typeof(p_binding->'task_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_binding->'input_digest') IS DISTINCT FROM 'string'
 OR coalesce(p_binding->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_binding->'profile') IS DISTINCT FROM 'object' THEN RETURN 'conflict';END IF;
 params:=p_binding->'params';transport:=p_binding->'transport';
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR (params-'artifact_id'-'sha256')<>'{}'
 OR jsonb_typeof(params->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(params->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(params->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(transport) IS DISTINCT FROM 'object'
 OR transport->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (transport-'schema_version'-'rows'-'manifest'-'chunks'-'direct'-'count'-'entries_sha')<>'{}'
 OR jsonb_typeof(transport->'count') IS DISTINCT FROM 'number'
 OR coalesce(transport->>'count','') !~ '^[0-9]+$'
 OR jsonb_typeof(transport->'entries_sha') IS DISTINCT FROM 'string'
 OR coalesce(transport->>'entries_sha','') !~ '^[0-9a-f]{64}$'
 OR (p_binding->>'task_id')::uuid='00000000-0000-0000-0000-000000000000'
 OR (params->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000' THEN RETURN 'conflict';END IF;
 count_entries:=(transport->>'count')::integer;
 IF count_entries NOT BETWEEN 3 AND 4097 THEN RETURN 'conflict';END IF;
 RETURN photo.semantic_defect_completed_registration_state(p_tenant,p_run,(p_binding->>'task_id')::uuid,
 (params->>'artifact_id')::uuid,params->>'sha256',p_binding->>'input_digest',p_binding->'profile',
 transport->'rows',transport->'manifest',transport->'chunks',transport->'direct',count_entries,transport->>'entries_sha');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN 'conflict';
END $$;
ALTER FUNCTION photo.semantic_defect_completed_binding_state(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_completed_binding_state(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_completed_binding_state(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- Composed retained-evidence locks only. Does not seal/queue, establish actual
-- pixel coverage or replace remaining completeness/root/content obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_completed_base(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;project uuid;actor uuid;session_id uuid;
 binding jsonb;config jsonb;native jsonb;origin jsonb;version uuid;source uuid;source_sha text;params uuid;params_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->'tenant_id' IS DISTINCT FROM to_jsonb(p_tenant::text) OR p_evidence->'run_id' IS DISTINCT FROM to_jsonb(p_run::text)
 OR (p_evidence-ARRAY['schema_version','tenant_id','project_id','actor_id','session_id','run_id','parent_run_id','request_sha','params_document_id','config_text','policy_text','source_authority','origin_pins','origin_refs','files','params_pin','content','faces','parser','manual','person','registration'])<>'{}'
 OR jsonb_typeof(p_evidence->'config_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'config_text') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_evidence->'policy_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'policy_text') NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' THEN RETURN false;END IF;
 config:=(p_evidence->>'config_text')::jsonb;native:=config->'v27_defect_prepare';
 IF config IS DISTINCT FROM r.config_snapshot OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(p_evidence->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR (p_evidence->>'policy_text')::jsonb IS DISTINCT FROM p_evidence->'source_authority'->'policy'
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR native->'parent_run_id' IS DISTINCT FROM p_evidence->'parent_run_id' OR native->'request_sha' IS DISTINCT FROM p_evidence->'request_sha'
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text OR native->>'policy_sha' IS DISTINCT FROM w.policy_sha
 OR native->'source' IS DISTINCT FROM p_evidence->'source_authority'->'binding'
 OR native->'params' IS DISTINCT FROM p_evidence->'content'->'params' OR native->'params' IS DISTINCT FROM p_evidence->'registration'->'params'
 OR native->'task_id' IS DISTINCT FROM p_evidence->'registration'->'task_id' OR native->'input_digest' IS DISTINCT FROM p_evidence->'registration'->'input_digest' THEN RETURN false;END IF;
 binding:=native->'source';version:=(binding->>'asset_version_id')::uuid;source:=(binding->'source'->>'artifact_id')::uuid;source_sha:=binding->'source'->>'sha256';
 params:=(native->'params'->>'artifact_id')::uuid;params_sha:=native->'params'->>'sha256';origin:=p_evidence->'origin_pins';
 IF p_evidence->'origin_refs' IS DISTINCT FROM jsonb_build_array(
 jsonb_build_object('artifact_id',origin->1->'artifact_id','sha256',origin->1->'sha256'),jsonb_build_object('artifact_id',origin->2->'artifact_id','sha256',origin->2->'sha256'),
 jsonb_build_object('artifact_id',origin->3->'artifact_id','sha256',origin->3->'sha256'),jsonb_build_object('artifact_id',origin->4->'artifact_id','sha256',origin->4->'sha256'),jsonb_build_object('artifact_id',origin->0->'artifact_id','sha256',origin->0->'sha256')) THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_source_authority(p_tenant,project,actor,session_id,version,source,source_sha,p_evidence->'source_authority') IS NOT TRUE
 OR photo.lock_semantic_defect_content_provenance(p_tenant,project,actor,session_id,version,source,source_sha,(p_evidence->>'params_document_id')::uuid,params,params_sha,origin,p_evidence->'params_pin',p_evidence->'content') IS NOT TRUE
 OR photo.lock_semantic_defect_execution_files(p_tenant,project,actor,session_id,version,source,source_sha,p_evidence->'origin_refs',p_evidence->'files') IS NOT TRUE
 OR photo.lock_semantic_defect_selected_producers(p_tenant,project,actor,session_id,version,source,source_sha,p_evidence->'faces',p_evidence->'parser',p_evidence->'manual') IS NOT TRUE
 OR photo.lock_semantic_defect_person_authority(p_tenant,project,actor,session_id,version,source,source_sha,p_evidence->'person',p_evidence->'content') IS NOT TRUE
 OR photo.semantic_defect_completed_binding_state(p_tenant,p_run,p_evidence->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,project,actor,session_id,version,source,source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_completed_base(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_completed_base(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_completed_base(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_completed_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.lock_semantic_defect_admission_anchor(p_tenant,p_run,p_evidence) IS NOT TRUE
 OR photo.semantic_defect_policy_expectations_match(p_evidence) IS NOT TRUE
 OR photo.semantic_defect_admission_closure_matches(p_evidence->'files',p_evidence->'content',p_evidence->'registration') IS NOT TRUE
 OR photo.semantic_defect_landmark_inventory_matches(p_evidence->'files',p_evidence->'content',p_evidence->'faces') IS NOT TRUE
 OR photo.semantic_defect_mask_root_inventory_matches(p_evidence->'content',p_evidence->'parser',p_evidence->'manual') IS NOT TRUE THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_completed_base(p_tenant,p_run,p_evidence-'anchor') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_skin_lineage(p_tenant,p_evidence);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
ALTER FUNCTION photo.seal_semantic_defect_preparation(uuid,uuid,jsonb) RENAME TO seal_semantic_defect_preparation_pre_completed;
REVOKE ALL ON FUNCTION photo.seal_semantic_defect_preparation_pre_completed(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_semantic_defect_preparation(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sealed boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT graph_sealed INTO sealed FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF sealed AND photo.lock_semantic_defect_completed_evidence(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN NULL;END IF;
 RETURN photo.seal_semantic_defect_preparation_pre_completed(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.seal_semantic_defect_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_defect_preparation(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_defect_preparation(uuid,uuid,jsonb) TO photo_scheduler;
REVOKE ALL ON FUNCTION photo.semantic_defect_completed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text) FROM photo_scheduler;
REVOKE ALL ON FUNCTION photo.semantic_defect_completed_binding_state(uuid,uuid,jsonb) FROM photo_scheduler;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_completed_base(uuid,uuid,jsonb) FROM photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
