-- A sealed V2 snapshot's root ArtifactSnapshot/InputSnapshot reference is as
-- immutable as its items. Pending construction still may attach the root.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.guard_v22_frozen_snapshot() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='v2' AND OLD.expires_at IS NULL THEN
  IF TG_OP='DELETE' THEN
   IF photo.v22_tenant_purge_active(OLD.tenant_id) THEN RETURN OLD; END IF;
   RAISE EXCEPTION 'V22_FROZEN_SNAPSHOT_IMMUTABLE' USING ERRCODE='23514';
  END IF;
  IF NEW.expires_at IS DISTINCT FROM OLD.expires_at OR
   NEW.artifact_id IS DISTINCT FROM OLD.artifact_id OR
   NEW.created_at IS DISTINCT FROM OLD.created_at THEN
   RAISE EXCEPTION 'V22_FROZEN_SNAPSHOT_IMMUTABLE' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_frozen_snapshot() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_frozen_snapshot() FROM PUBLIC;
