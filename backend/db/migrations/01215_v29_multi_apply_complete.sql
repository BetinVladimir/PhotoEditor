-- +goose Up
-- Complete atomic Apply: original and output locators must match completed reads.
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v29_multi_complete(p_tenant uuid,p_project uuid,p_operation text,p_task uuid,
 p_actor uuid,p_session uuid,p_decision jsonb,p_root text,p_image text,p_witnesses jsonb,p_originals jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity uuid;
BEGIN
 identity:=photo.apply_v29_multi_retained(p_tenant,p_project,p_operation,p_task,p_actor,p_session,p_decision,p_root,p_image,p_witnesses);
 IF identity IS NULL THEN RETURN NULL;END IF;
 IF photo.v29_multi_applied_originals_current(p_tenant,(p_decision->>'quality_run_id')::uuid,p_originals) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'MULTI_APPLY_ORIGINALS_STALE' USING ERRCODE='40001';
 END IF;
 RETURN identity;
END $$;
ALTER FUNCTION photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v29_multi_complete(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb,jsonb) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb) FROM photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
