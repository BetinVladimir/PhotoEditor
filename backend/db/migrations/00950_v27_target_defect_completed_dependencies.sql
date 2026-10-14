-- +goose Up
-- Additive completed-state barrier; canonical consumer locks remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_defect_completed_dependencies(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_namespace uuid,p_producers jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;e jsonb;c jsonb;first_config jsonb;
 op text;marker text;v_run_id uuid;v_task_id uuid;v_result_id uuid;i integer;
BEGIN
 IF p_namespace IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_producers) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_producers)<>3 THEN RETURN false;END IF;
 FOR i IN 0..2 LOOP
  e:=p_producers->i;
  IF jsonb_typeof(e) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  op:=CASE i WHEN 0 THEN 'mask.person' WHEN 1 THEN 'face.landmarks' ELSE 'mask.face_skin' END;
  marker:=CASE i WHEN 0 THEN 'v27_target_person_prepare' WHEN 1 THEN 'v27_target_face_prepare' ELSE 'v27_target_skin_prepare' END;
  v_run_id:=(e->>'run_id')::uuid;v_task_id:=(e->>'task_id')::uuid;v_result_id:=(e->>'result_id')::uuid;
  IF e->>'operation' IS DISTINCT FROM op OR v_result_id IS NULL
  OR v_run_id IS DISTINCT FROM photo.semantic_uuid_v5(p_namespace,'semantic.'||op||':reference')
  OR v_task_id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(v_run_id,'workflow'),op||':0') THEN RETURN false;END IF;
  SELECT rr.* INTO r FROM photo.runs rr WHERE rr.tenant_id=p_tenant AND rr.id=v_run_id FOR SHARE;
  SELECT tt.* INTO t FROM photo.tasks tt WHERE tt.tenant_id=p_tenant AND tt.run_id=r.id AND tt.id=v_task_id FOR SHARE;
  c:=r.config_snapshot->marker;
  IF r.id IS NULL OR t.id IS NULL OR c IS NULL OR r.kind<>'v2' OR r.status<>'completed'
  OR r.graph_sealed IS NOT TRUE OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
  OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
  OR c->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR c->>'target_namespace' IS DISTINCT FROM p_namespace::text
  OR c->>'target_stage_id' IS DISTINCT FROM photo.semantic_uuid_v5(p_namespace,'stage:'||op)::text
  OR c->>'task_id' IS DISTINCT FROM v_task_id::text
  OR t.operation IS DISTINCT FROM op OR t.operation_version IS DISTINCT FROM '1'
  OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS DISTINCT FROM v_result_id THEN RETURN false;END IF;
  IF i=0 THEN first_config:=c;
  ELSIF c->'parent_run_id' IS DISTINCT FROM first_config->'parent_run_id'
  OR c->'request_sha' IS DISTINCT FROM first_config->'request_sha'
  OR c->'manifest_sha' IS DISTINCT FROM first_config->'manifest_sha'
  OR c->'target_ordinal' IS DISTINCT FROM first_config->'target_ordinal'
  OR c->'source' IS DISTINCT FROM first_config->'source'
  OR c->'intent' IS DISTINCT FROM first_config->'intent'
  OR c->'snapshot' IS DISTINCT FROM first_config->'snapshot' THEN RETURN false;END IF;
  PERFORM id FROM photo.task_results tr WHERE tr.tenant_id=p_tenant AND tr.task_id=t.id AND tr.id=v_result_id
   AND attempt=t.attempt AND fencing_token=t.fencing_token FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_defect_completed_dependencies(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_completed_dependencies(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_defect_completed_dependencies(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
