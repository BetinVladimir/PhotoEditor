-- +goose Up
-- Bind the persisted original producer to the retained original evidence under
-- the existing admission locks. Legacy snapshots keep their explicit-pin path.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_defect_native_parent_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb; child uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_anchor) IS DISTINCT FROM 'object' OR octet_length(p_anchor::text)>8192
 OR (p_anchor-'parent_run_id'-'request_sha'-'auth_session_id'-'source'-'intent'-'snapshot')<>'{}'
 OR p_anchor->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR coalesce(p_anchor->>'request_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT r.id,r.config_snapshot->'v27_defect_prepare' INTO child,native
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='defects.propose' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND r.graph_sealed AND NOT r.cancel_requested
 AND r.status IN('running','waiting_review','completed') AND w.project_id=p_project AND w.workflow_type='semantic' AND w.renderer_version='v2.2';
 IF NOT FOUND THEN RETURN false;END IF;
 IF native ? 'original_producer' AND (jsonb_typeof(native->'original_producer') IS DISTINCT FROM 'object'
 OR (native->'original_producer'-'producer_task_id'-'params_sha256')<>'{}'
 OR jsonb_typeof(native->'original_producer'->'producer_task_id') IS DISTINCT FROM 'string'
 OR (native->'original_producer'->>'producer_task_id')::uuid IS NULL
 OR coalesce(native->'original_producer'->>'params_sha256','') !~ '^[0-9a-f]{64}$') THEN RETURN false;END IF;
 RETURN child=photo.semantic_uuid_v5((p_anchor->>'parent_run_id')::uuid,'semantic.defect.prepare:reference')
 AND native->'schema_version'='1'::jsonb AND native->>'task_id'=p_task::text
 AND (native-'schema_version'-'params'-'task_id'-'input_digest'-'policy_sha'-'original_producer')=p_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;

CREATE FUNCTION photo.semantic_defect_saved_origin_matches(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb;selection jsonb;origin jsonb;producer uuid;params_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT config_snapshot->'v27_defect_prepare' INTO native FROM photo.runs
 WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR native IS DISTINCT FROM (p_evidence->>'config_text')::jsonb->'v27_defect_prepare' THEN RETURN false;END IF;
 IF NOT (native ? 'original_producer') THEN RETURN true;END IF;
 selection:=native->'original_producer';origin:=p_evidence->'origin_pins';
 IF jsonb_typeof(selection) IS DISTINCT FROM 'object'
 OR (selection-'producer_task_id'-'params_sha256')<>'{}'
 OR jsonb_typeof(selection->'producer_task_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(selection->'params_sha256') IS DISTINCT FROM 'string'
 OR coalesce(selection->>'params_sha256','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(origin) IS DISTINCT FROM 'array' OR jsonb_array_length(origin)<>5 THEN RETURN false;END IF;
 SELECT t.id,p.sha256 INTO producer,params_sha FROM photo.tasks t
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND a.id=(origin->1->>'artifact_id')::uuid AND a.sha256=origin->1->>'sha256'
 AND p.id=(origin->0->>'artifact_id')::uuid AND p.sha256=origin->0->>'sha256'
 FOR SHARE OF t,a,p;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN producer=(selection->>'producer_task_id')::uuid AND params_sha=selection->>'params_sha256';
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_saved_origin_matches(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_saved_origin_matches(uuid,uuid,jsonb) FROM PUBLIC;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_saved_origin;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_saved_origin(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.lock_semantic_defect_admission_evidence_pre_saved_origin(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.semantic_defect_saved_origin_matches(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.lock_semantic_defect_completed_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_completed_evidence_pre_saved_origin;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_completed_evidence_pre_saved_origin(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_completed_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.lock_semantic_defect_completed_evidence_pre_saved_origin(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.semantic_defect_saved_origin_matches(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_completed_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_completed_evidence(uuid,uuid,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
