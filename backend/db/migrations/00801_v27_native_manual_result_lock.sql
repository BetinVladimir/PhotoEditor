-- +goose Up
-- Complete manual authority is locked before invoking unchanged native result checks.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_manual_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE admission jsonb;face jsonb;run_id uuid;native_completed jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR octet_length(p_expected::text)>33554432
 OR (p_expected-ARRAY['schema_version','admission','result_id','outputs','params','files'])<>'{}'
 OR p_expected->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 admission:=p_expected->'admission';
 face:=admission->'native'->'landmarks'->'admission'->'face';
 IF face->>'tenant_id' IS DISTINCT FROM p_tenant::text OR face->>'project_id' IS DISTINCT FROM p_project::text
 OR face->>'actor_id' IS DISTINCT FROM p_actor::text OR face->>'session_id' IS DISTINCT FROM p_session::text
 OR face->>'parent_run_id' IS DISTINCT FROM p_parent::text OR face->>'request_sha' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 run_id:=photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference');
 IF photo.lock_semantic_native_manual_admission_evidence(p_tenant,run_id,admission) IS NOT TRUE THEN RETURN false;END IF;
 -- All original manual evidence locks remain held in this caller transaction.
 native_completed:=jsonb_set(p_expected,'{admission}',admission->'native',false);
 RETURN photo.lock_semantic_native_defect_result(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task,native_completed) IS TRUE;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_manual_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_manual_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_manual_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
