-- +goose Up
-- Exact request binding for a lost queued response. The caller must still read
-- the donor queued receipt under current consent, source and qualification.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_open_eyes_queued_request_matches(p_tenant uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_intent_sha text) RETURNS boolean LANGUAGE plpgsql
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_run IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR coalesce(p_intent_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT r.config_snapshot INTO config FROM photo.runs r
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.graph_sealed AND r.v28_donor_queued_receipt IS NOT NULL
  AND NOT r.cancel_requested
  AND r.created_by=p_actor FOR SHARE;
 IF NOT FOUND OR config->'v28_open_eyes'->>'actor_id' IS DISTINCT FROM p_actor::text
 OR config->'v28_open_eyes'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR config->'v28_open_eyes'->>'client_intent_sha' IS DISTINCT FROM p_intent_sha
 OR config->'v28_open_eyes'->>'run_id' IS DISTINCT FROM p_run::text
 THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_open_eyes_queued_request_matches(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_open_eyes_queued_request_matches(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_open_eyes_queued_request_matches(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
