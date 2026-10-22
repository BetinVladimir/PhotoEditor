-- +goose Up
-- Pure shared wire validation only. Existing admission functions are unchanged
-- until the copy document and receipt comparisons adopt this helper together.
-- +goose StatementBegin
CREATE FUNCTION photo.shown_copy_fence_wire_value(p_value jsonb)
RETURNS bigint LANGUAGE plpgsql IMMUTABLE STRICT
SET search_path=pg_catalog,photo AS $$
DECLARE kind text; digits text; parsed numeric;
BEGIN
 kind:=jsonb_typeof(p_value);
 IF kind NOT IN ('number','string') THEN RETURN NULL;END IF;
 digits:=p_value #>> '{}';
 IF digits IS NULL OR digits !~ '^[1-9][0-9]{0,18}$' THEN RETURN NULL;END IF;
 parsed:=digits::numeric;
 IF parsed>9223372036854775807::numeric THEN RETURN NULL;END IF;
 IF (kind='number' AND parsed>9007199254740991::numeric)
 OR (kind='string' AND parsed<=9007199254740991::numeric) THEN RETURN NULL;END IF;
 RETURN parsed::bigint;
END $$;
ALTER FUNCTION photo.shown_copy_fence_wire_value(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.shown_copy_fence_wire_value(jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission;
-- Only owner-owned admission functions can use this pure helper. No table
-- privileges, lease/approval authority or public execution is introduced.
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
