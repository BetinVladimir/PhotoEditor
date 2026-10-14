-- +goose Up
-- Protect completed Render responses in the shared ledger; expiry/DELETE
-- retention and tenant purge remain governed by their existing lifecycles.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_render_response() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.response_status=202 AND OLD.route='V2.Admit' AND OLD.resource_id IS NOT NULL THEN
  IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=OLD.tenant_id AND id=OLD.resource_id
   AND kind='bulk' AND v26_submission_request_sha=OLD.request_hash)
   AND ROW(NEW.id,NEW.tenant_id,NEW.principal_type,NEW.principal_id,NEW.route,NEW.key,
    NEW.request_hash,NEW.resource_id,NEW.response_status,NEW.response_body)
   IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.principal_type,OLD.principal_id,OLD.route,OLD.key,
    OLD.request_hash,OLD.resource_id,OLD.response_status,OLD.response_body) THEN
   RAISE EXCEPTION 'V26_RENDER_RESPONSE_IMMUTABLE' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v26_render_response() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v26_render_response() FROM PUBLIC;
CREATE TRIGGER guard_v26_render_response BEFORE UPDATE ON photo.idempotency_records
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_render_response();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
