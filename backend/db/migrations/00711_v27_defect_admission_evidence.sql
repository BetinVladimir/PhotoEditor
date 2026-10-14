-- +goose Up
-- Composed retained-evidence locks only. Does not seal/queue, establish actual
-- pixel coverage or replace remaining completeness/root/content obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
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
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
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
 OR photo.semantic_defect_registration_binding_state(p_tenant,p_run,p_evidence->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,project,actor,session_id,version,source,source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
