-- +goose Up
-- A boolean refusal must roll back every write made by the installed adopter,
-- including versions already inserted for earlier items in the collection.
ALTER FUNCTION photo.adopt_apply_results(uuid,uuid,uuid)
 RENAME TO adopt_apply_results_before_atomic_rejection;
REVOKE ALL ON FUNCTION photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.adopt_apply_results(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 BEGIN
  IF photo.adopt_apply_results_before_atomic_rejection(p_tenant,p_run,p_token) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'APPLY_ADOPTION_REJECTED' USING ERRCODE='PA441';
  END IF;
 EXCEPTION WHEN SQLSTATE 'PA441' THEN
  -- Entering this handler rolls back the complete inner subtransaction.
  RETURN false;
 END;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_apply_results(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
