-- +goose Up
-- Acquire the whole project graph before per-source/per-intent proof checks.
-- Caller rolls back on false and retains this transaction through admission.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_negative_replacement_scope(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE settlement jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 -- Existing authority independently recomputes closure and retains all project
 -- run/task locks in UUID order before checking the immutable decision again.
 settlement:=photo.reconcile_v29_negative_invalidation(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,4096,65536);
 RETURN settlement IS NOT NULL AND settlement->>'settled'='true';
END $$;
ALTER FUNCTION photo.lock_v29_negative_replacement_scope(uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_replacement_scope(uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_negative_replacement_scope(uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.read_v29_negative_frozen_intent_locked(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_project,'review_id',p_review,
 'decision_hash',p_hash,'original_task_id',t.id,'original_run_id',t.run_id,'operation',t.operation,
 'params',jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)) INTO result
 FROM photo.v29_negative_invalidations i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND w.project_id=p_project
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=a.id AND d.sha256=a.sha256
 AND d.bytes=a.bytes AND d.bucket=a.bucket AND d.object_version=a.object_version
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.review_id=p_review AND i.decision_hash=p_hash
 AND t.operation IN('optical.negative','optical.perspective','optical.panorama','optical.focus_stack') AND t.operation_version='1'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='task_params' AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 65536 AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v29_negative_review_selection(p_tenant,i.origin_task_id) selected
 WHERE selected.review_id=p_review AND selected.decision_hash=p_hash AND selected.route=i.route) FOR SHARE OF t,a,d;
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_frozen_intent_locked(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_frozen_intent_locked(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;


CREATE FUNCTION photo.lock_v29_negative_frozen_intent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_task uuid,p_expected jsonb,p_original text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF p_expected IS NULL OR jsonb_typeof(p_expected)<>'object' OR pg_column_size(p_expected)>32768
 OR p_original IS NULL OR octet_length(p_original) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 actual:=photo.read_v29_negative_frozen_intent_locked(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 RETURN actual IS NOT NULL AND actual=p_expected
 AND octet_length(p_original)=(actual->'params'->>'bytes')::bigint
 AND encode(sha256(convert_to(p_original,'UTF8')),'hex')=actual->'params'->>'sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
