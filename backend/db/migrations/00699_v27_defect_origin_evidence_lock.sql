-- +goose Up
-- Original canonical defect producer and exact privately retained object pins.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_origin_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; a photo.artifacts%ROWTYPE; producer photo.tasks%ROWTYPE; actual jsonb; ordinal integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_pins) IS DISTINCT FROM 'array' OR pg_column_size(p_pins)>4194304 THEN RETURN false; END IF;
 IF jsonb_array_length(p_pins)<>5
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_pins))<>5 THEN RETURN false; END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_pins) LOOP
  IF pin->>'role' IS DISTINCT FROM (ARRAY['params','proposal','report','labels','protection'])[ordinal+1] THEN RETURN false; END IF;
  ordinal:=ordinal+1;
 END LOOP;
 SELECT task.* INTO producer FROM photo.tasks task
 JOIN photo.artifacts proposal ON proposal.tenant_id=task.tenant_id AND proposal.created_by_task_id=task.id
 WHERE task.tenant_id=p_tenant AND proposal.id=(p_pins->1->>'artifact_id')::uuid
 AND proposal.sha256=p_pins->1->>'sha256' FOR SHARE OF task;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=producer.run_id FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=producer.run_id ORDER BY id FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id AND task_id=producer.id FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=producer.id
 AND attempt=producer.attempt AND fencing_token=producer.fencing_token FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id IN(SELECT (value->>'artifact_id')::uuid FROM jsonb_array_elements(p_pins)) ORDER BY id FOR SHARE;
 IF photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
 (p_pins->1->>'artifact_id')::uuid,p_pins->1->>'sha256',
 (p_pins->2->>'artifact_id')::uuid,p_pins->2->>'sha256',
 (p_pins->3->>'artifact_id')::uuid,p_pins->3->>'sha256',
 (p_pins->4->>'artifact_id')::uuid,p_pins->4->>'sha256',
 (p_pins->0->>'artifact_id')::uuid,p_pins->0->>'sha256') IS NOT TRUE THEN RETURN false; END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(p_pins) LOOP
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->>'artifact_id')::uuid;
  IF NOT FOUND THEN RETURN false; END IF;
  actual:=jsonb_build_object('role',pin->>'role','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type);
  IF actual IS DISTINCT FROM pin THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_origin_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_origin_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_origin_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
