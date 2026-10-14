-- +goose Up
-- Narrow completed target parser prerequisite barrier; no table grants/backfill.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_skin_parser_completed(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_task uuid,p_result uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 c:=r.config_snapshot->'v27_target_face_prepare';
 IF r.id IS NULL OR t.id IS NULL OR c IS NULL OR r.kind<>'v2' OR r.status<>'completed'
 OR NOT r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR c->>'auth_session_id' IS DISTINCT FROM p_session::text OR c->>'operation' IS DISTINCT FROM 'face.parse'
 OR t.operation<>'face.parse' OR t.state<>'succeeded' OR t.cancel_requested
 OR t.canonical_result_id IS DISTINCT FROM p_result OR p_result IS NULL
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.face.parse:reference')
 OR p_task IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(p_run,'workflow'),'face.parse:0') THEN RETURN false;END IF;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task AND id=p_result
 AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_skin_parser_completed(uuid,uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_skin_parser_completed(uuid,uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_skin_parser_completed(uuid,uuid,uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
