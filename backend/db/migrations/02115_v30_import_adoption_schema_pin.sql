-- +goose Up
-- A completed import must be reconstructed under the exact frozen contracts
-- that its worker used. A locally pinned schema file alone is insufficient.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_schema_pin(p_tenant uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_result uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;pin text;
BEGIN
 authority:=photo.v30_interchange_import_adoption_context(
  p_tenant,p_task,p_attempt,p_fence,p_result);
 IF authority IS NULL THEN RETURN NULL;END IF;
 SELECT t.execution_profile->>'contracts_sha' INTO pin FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE;
 IF coalesce(pin,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 RETURN pin;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_schema_pin(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_schema_pin(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_schema_pin(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
