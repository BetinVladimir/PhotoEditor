-- +goose Up
-- Current source mapping authority retained through eventual admission.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_negative_affected_sources(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_task uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;intent jsonb;
BEGIN
 IF p_expected IS NULL OR jsonb_typeof(p_expected) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_expected)>30 OR pg_column_size(p_expected)>32768 THEN RETURN false;END IF;
 IF photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 intent:=photo.read_v29_negative_frozen_intent_locked(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 IF intent IS NULL THEN RETURN false;END IF;
 actual:=photo.read_v29_negative_affected_source_inputs(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 IF actual IS NULL OR actual IS DISTINCT FROM p_expected THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id IN(SELECT (source->>'artifact_id')::uuid FROM jsonb_array_elements(actual) source)
 ORDER BY id FOR SHARE;
 RETURN photo.read_v29_negative_affected_source_inputs(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task) IS NOT DISTINCT FROM p_expected;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_negative_affected_sources(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_affected_sources(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_negative_affected_sources(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
