-- +goose Up
-- Membership is independently checked from persisted decision/original intent,
-- deterministic identities and the replacement run marker. This reader never
-- creates a run/task or accepts an execution/qualification profile.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_replacement_member(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_original uuid,p_task uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;namespace uuid;
BEGIN
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_original);
 IF intent IS NULL OR p_task IS NULL OR p_run IS NULL THEN RETURN false;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement:'||p_project::text||':'||p_review::text||':'||p_hash);
 IF p_task IS DISTINCT FROM photo.v30_uuid5(namespace,'task:'||p_original::text)
 OR p_run IS DISTINCT FROM photo.v30_uuid5(namespace,'run:'||(intent->>'original_run_id')) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v29_negative_invalidations invalid
 JOIN photo.v29_negative_review_selection(p_tenant,invalid.origin_task_id) selected
 ON selected.review_id=invalid.review_id AND selected.decision_hash=invalid.decision_hash AND selected.route='base'
 JOIN photo.tasks original ON original.tenant_id=invalid.tenant_id AND original.id=invalid.task_id
 JOIN photo.tasks replacement ON replacement.tenant_id=original.tenant_id AND replacement.id=p_task AND replacement.run_id=p_run
 JOIN photo.runs r ON r.tenant_id=replacement.tenant_id AND r.id=replacement.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.project_id=p_project
 JOIN photo.backend_documents document ON document.tenant_id=replacement.tenant_id AND document.owner_id=replacement.id
 AND document.run_id=r.id AND document.purpose='task_params' AND document.state='ready' AND document.artifact_id=replacement.params_artifact_id
 JOIN photo.artifacts params ON params.tenant_id=document.tenant_id AND params.id=document.artifact_id
 AND params.sha256=document.sha256 AND params.bytes=document.bytes AND params.bucket=document.bucket AND params.object_version=document.object_version
 WHERE invalid.tenant_id=p_tenant AND invalid.review_id=p_review AND invalid.decision_hash=p_hash AND invalid.route='base' AND invalid.task_id=p_original
 AND original.state IN('succeeded','failed','cancelled','skipped')
 AND replacement.operation=original.operation AND replacement.operation_version='1' AND original.operation_version='1'
 AND NOT replacement.cancel_requested AND NOT r.cancel_requested AND r.kind='v2' AND r.graph_sealed
 AND r.status IN('queued','running','waiting_review','completed')
 AND r.config_snapshot->'v29_negative_replacement'=jsonb_build_object('version','v29_negative_replacement_v1',
 'project_id',p_project::text,'review_id',p_review::text,'decision_hash',p_hash,'original_run_id',original.run_id::text)
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.output_role='task_params'
 AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 65536 AND params.object_version NOT IN('','null')
 AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations stale WHERE stale.tenant_id=replacement.tenant_id AND stale.task_id=replacement.id));
END $$;
ALTER FUNCTION photo.v29_negative_replacement_member(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_replacement_member(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_negative_replacement_member(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
