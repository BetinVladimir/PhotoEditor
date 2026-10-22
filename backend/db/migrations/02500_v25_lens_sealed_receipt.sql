-- +goose Up
-- Lost-202 recovery is an exact immutable submission receipt, never run-ID reuse.
-- +goose StatementBegin
CREATE FUNCTION photo.replay_v25_lens_draft(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_run uuid,p_workflow uuid,p_config_jcs text,p_policy_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; config jsonb; config_sha text;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_snapshot IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_run IS NULL OR p_workflow IS NULL OR p_config_jcs IS NULL
 OR octet_length(p_config_jcs) NOT BETWEEN 2 AND 1048576
 OR coalesce(p_policy_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
 OR r.created_by IS DISTINCT FROM p_actor OR r.input_snapshot_id IS DISTINCT FROM p_snapshot THEN RETURN false; END IF;
 config:=p_config_jcs::jsonb;
 config_sha:=encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex');
 IF r.config_hash IS DISTINCT FROM config_sha OR r.config_snapshot IS DISTINCT FROM config
 OR config->>'project_id' IS DISTINCT FROM p_project::text
 OR config->>'input_snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR config->'v25_lens'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=p_workflow
 AND w.run_id=p_run AND w.project_id=p_project AND w.policy_sha=p_policy_sha) THEN RETURN false; END IF;
 -- This repeats live session/ACL/source authority and the full task/profile plan.
 RETURN photo.v25_lens_activation_ready(p_tenant,p_run);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.replay_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replay_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replay_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
