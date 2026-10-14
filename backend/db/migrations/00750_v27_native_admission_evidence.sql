-- +goose Up
-- Complete native retained-evidence lock composition. No activation or receipt.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;config jsonb;native jsonb;operation text;config_key text;project uuid;actor uuid;session_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432
 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR p_evidence->>'run_id' IS DISTINCT FROM p_run::text
 OR (p_evidence-ARRAY['schema_version','operation','tenant_id','project_id','actor_id','session_id','run_id','parent_run_id','request_sha','config_text','policy_text','anchor','binding','icc_sha','unit_rgb','source_pins','expected','qualification','params_document_id','params_pin','files','registration'])<>'{}'
 OR jsonb_typeof(p_evidence->'config_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'config_text') NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_evidence->'policy_text') IS DISTINCT FROM 'string' OR octet_length(p_evidence->>'policy_text') NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
 operation:=p_evidence->>'operation';
 IF operation IS NULL OR operation NOT IN ('face.detect','mask.person') THEN RETURN false;END IF;
 config_key:=CASE operation WHEN 'face.detect' THEN 'v27_detector_prepare' ELSE 'v27_person_prepare' END;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;
 IF photo.lock_semantic_native_admission_anchor(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 config:=(p_evidence->>'config_text')::jsonb;native:=config->config_key;
 IF config IS DISTINCT FROM r.config_snapshot OR (config-ARRAY['project_id',config_key])<>'{}'
 OR config->>'project_id' IS DISTINCT FROM project::text
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(p_evidence->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha
 OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (native-ARRAY['schema_version','parent_run_id','request_sha','auth_session_id','source','intent','snapshot','params_document','params','task_id','input_digest','policy_sha'])<>'{}'
 OR native->'parent_run_id' IS DISTINCT FROM p_evidence->'parent_run_id' OR native->'request_sha' IS DISTINCT FROM p_evidence->'request_sha'
 OR native->>'auth_session_id' IS DISTINCT FROM session_id::text OR native->>'policy_sha' IS DISTINCT FROM w.policy_sha
 OR native->'source' IS DISTINCT FROM p_evidence->'binding'
 OR native->'params_document' IS DISTINCT FROM p_evidence->'params_document_id'
 OR native->'params' IS DISTINCT FROM p_evidence->'registration'->'params'
 OR native->'task_id' IS DISTINCT FROM p_evidence->'registration'->'task_id'
 OR native->'input_digest' IS DISTINCT FROM p_evidence->'registration'->'input_digest' THEN RETURN false;END IF;
 IF photo.lock_semantic_native_source_authority(p_tenant,project,actor,session_id,p_evidence) IS NOT TRUE
 OR photo.semantic_native_registration_binding_state(p_tenant,p_run,operation,p_evidence->'registration') IS DISTINCT FROM 'replayed' THEN RETURN false;END IF;
 RETURN photo.lock_semantic_native_graph_budget(p_tenant,p_run,p_evidence);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
