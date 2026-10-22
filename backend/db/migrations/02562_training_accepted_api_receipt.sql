-- +goose Up
-- API recovery of an accepted original command requires no scheduler/provider.
-- Pending recovery manifests remain private to photo_scheduler.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_accepted_receipt(p_tenant uuid,p_actor uuid,
 p_session uuid,p_profile uuid,p_run uuid,p_key text,p_sha text)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT i.response_body FROM photo.idempotency_records i
 JOIN photo.runs r ON r.tenant_id=i.tenant_id AND r.id=i.resource_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor
 AND i.route='V2.Admit' AND i.key=p_key AND i.request_hash=p_sha
 AND p_key COLLATE "C" ~ '^[!-~]{1,128}$' AND p_sha ~ '^[0-9a-f]{64}$'
 AND i.response_status=202 AND r.id=p_run AND r.created_by=p_actor
 AND r.kind='v2' AND r.graph_sealed AND w.workflow_type='learning'
 AND r.config_snapshot->'v23_training'->>'profile_id'=p_profile::text
 AND photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
 AND i.response_body=jsonb_build_object('run_id',r.id,'workflow_id',w.id,'state','queued')
$$;
ALTER FUNCTION photo.v23_training_accepted_receipt(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_accepted_receipt(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v23_training_accepted_receipt(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
