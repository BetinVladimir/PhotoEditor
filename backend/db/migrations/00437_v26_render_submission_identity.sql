-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v26_submission_request_sha text
 CHECK(v26_submission_request_sha IS NULL OR v26_submission_request_sha ~ '^[0-9a-f]{64}$');
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_submission_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v26_submission_request_sha IS NOT NULL AND (current_user<>'photo_fn_owner' OR NEW.kind<>'bulk') THEN
   RAISE EXCEPTION 'V26_SUBMISSION_IDENTITY_PRIVATE' USING ERRCODE='42501';
  END IF;
 ELSIF NEW.v26_submission_request_sha IS DISTINCT FROM OLD.v26_submission_request_sha THEN
  IF OLD.v26_submission_request_sha IS NOT NULL OR current_user<>'photo_fn_owner' OR NEW.kind<>'bulk' THEN
   RAISE EXCEPTION 'V26_SUBMISSION_IDENTITY_IMMUTABLE' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_v26_submission_identity BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_submission_identity();
ALTER FUNCTION photo.guard_v26_submission_identity() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v26_submission_identity() FROM PUBLIC;

CREATE FUNCTION photo.bind_v26_render_submission(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_request_sha text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE;
BEGIN
 IF p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN 'expired'; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired'; END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired'; END IF;
 -- Preserve tenant/usage/parent order even for a standalone scheduler call.
 IF NOT EXISTS(SELECT 1 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session)) THEN RETURN 'expired'; END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.cancel_requested THEN RETURN 'expired'; END IF;
 IF parent.v26_submission_request_sha IS NOT NULL THEN
  RETURN CASE WHEN parent.v26_submission_request_sha=p_request_sha THEN 'bound' ELSE 'conflict' END;
 END IF;
 IF parent.status<>'draft' THEN RETURN 'expired'; END IF;
 UPDATE photo.runs SET v26_submission_request_sha=p_request_sha WHERE tenant_id=p_tenant AND id=p_batch;
 RETURN 'bound';
END $$;
ALTER FUNCTION photo.bind_v26_render_submission(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v26_render_submission(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_v26_render_submission(uuid,uuid,uuid,uuid,text) TO photo_scheduler;

CREATE FUNCTION photo.v26_render_submission_state(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(request_sha text,run_status text,cancel_requested boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.v26_submission_request_sha,r.status::text,r.cancel_requested
 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session) scoped
 JOIN photo.runs r ON r.tenant_id=p_tenant AND r.id=p_batch
$$;
ALTER FUNCTION photo.v26_render_submission_state(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_submission_state(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_submission_state(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
