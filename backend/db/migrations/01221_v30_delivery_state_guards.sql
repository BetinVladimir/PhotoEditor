-- +goose Up
ALTER TABLE photo.delivery_packages ADD CONSTRAINT delivery_manifest_required
 CHECK (state NOT IN('review','ready') OR manifest_artifact_id IS NOT NULL);
ALTER TABLE photo.delivery_items ADD CONSTRAINT delivery_reason_shape
 CHECK ((state IN('pending','ready') AND reason IS NULL) OR
        (state IN('failed','omitted') AND reason IS NOT NULL AND length(btrim(reason))>0));
CREATE TRIGGER immutable_delivery_package BEFORE UPDATE ON photo.delivery_packages
 FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('id','tenant_id','project_id','workflow_id','snapshot_id','policy_artifact_id','created_at');
CREATE TRIGGER immutable_delivery_item BEFORE UPDATE ON photo.delivery_items
 FOR EACH ROW EXECUTE FUNCTION photo.reject_content_update('id','tenant_id','project_id','package_id','ordinal','preset_id','preset_sha','required','created_at');
-- +goose StatementBegin
CREATE FUNCTION photo.delivery_package_transition_allowed(p_from text,p_to text,p_published boolean) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog AS $$
 SELECT COALESCE(CASE
 WHEN p_published THEN p_from='ready' AND p_to='ready'
 WHEN p_from=p_to THEN p_from IN('building','review','ready','failed','cancelled')
 WHEN p_from='building' THEN p_to IN('review','ready','failed','cancelled')
 WHEN p_from='review' THEN p_to IN('ready','failed','cancelled')
 WHEN p_from='ready' THEN p_to IN('failed','cancelled')
 ELSE false END,false)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.delivery_package_transition_allowed(text,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.delivery_package_transition_allowed(text,text,boolean) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_delivery_state() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_TABLE_NAME='delivery_packages' THEN
  IF NOT photo.delivery_package_transition_allowed(OLD.state,NEW.state,OLD.published_at IS NOT NULL) THEN
   RAISE EXCEPTION 'DELIVERY_STATE_TRANSITION' USING ERRCODE='23514';END IF;
  IF OLD.manifest_artifact_id IS NOT NULL AND NEW.manifest_artifact_id IS DISTINCT FROM OLD.manifest_artifact_id THEN
   RAISE EXCEPTION 'DELIVERY_MANIFEST_IMMUTABLE' USING ERRCODE='23514';END IF;
  IF OLD.published_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'DELIVERY_PUBLICATION_IMMUTABLE' USING ERRCODE='23514';END IF;
 ELSE
  IF OLD.state<>'pending' AND NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'DELIVERY_ITEM_TERMINAL' USING ERRCODE='23514';END IF;
  IF OLD.export_id IS NOT NULL AND NEW.export_id IS DISTINCT FROM OLD.export_id THEN
   RAISE EXCEPTION 'DELIVERY_EXPORT_IMMUTABLE' USING ERRCODE='23514';END IF;
 END IF;
 IF NEW IS DISTINCT FROM OLD AND NEW.revision<>OLD.revision+1 THEN
  RAISE EXCEPTION 'DELIVERY_REVISION_REQUIRED' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_delivery_state() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_delivery_state() FROM PUBLIC;
CREATE TRIGGER guard_delivery_state BEFORE UPDATE ON photo.delivery_packages FOR EACH ROW EXECUTE FUNCTION photo.guard_delivery_state();
CREATE TRIGGER guard_delivery_state BEFORE UPDATE ON photo.delivery_items FOR EACH ROW EXECUTE FUNCTION photo.guard_delivery_state();
-- These are integrity guards, not authorization/publication acceptance.
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
