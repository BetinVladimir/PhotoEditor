-- +goose Up
-- Read one caller-owned live draft; this is not a grant to publish or seal.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_local_recovery_owner(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(request jsonb,task_plan jsonb,policy_sha text,reserved_bytes bigint,config_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved photo.runs%ROWTYPE;owner jsonb;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_actor IS NULL OR p_session IS NULL THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO saved FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 owner:=saved.config_snapshot->'v28_local_portrait';
 IF owner->>'tenant_id' IS DISTINCT FROM p_tenant::text OR owner->>'run_id' IS DISTINCT FROM p_run::text
  OR owner->>'actor_id' IS DISTINCT FROM p_actor::text OR owner->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR photo.v28_local_owner_request_scope(owner) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT owner,saved.v28_local_task_plan,w.policy_sha,q.bytes,saved.config_hash
  FROM photo.v2_workflows w JOIN photo.quota_reservations q ON q.tenant_id=w.tenant_id
   AND q.holder_type='run' AND q.holder_id=w.run_id AND q.state='reserved' AND q.expires_at>clock_timestamp()
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.id=(owner->>'workflow_id')::uuid
   AND w.project_id=(owner->>'project_id')::uuid AND w.input_snapshot_id=saved.input_snapshot_id
   AND w.workflow_type='preview' AND w.renderer_version='v2.2' FOR SHARE OF w,q;
END $$;
ALTER FUNCTION photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
