-- +goose Up
-- Historical immutable qualifications retain unknown timing; no backfill.
ALTER TABLE photo.model_qualifications ADD COLUMN measured_p95_ms bigint
 CHECK(measured_p95_ms IS NULL OR (measured_p95_ms BETWEEN 1 AND 9007199254740991 AND trusted_import AND status='passed'));
-- +goose StatementBegin
CREATE FUNCTION photo.qualification_timing_ms(p_metrics jsonb) RETURNS bigint
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE median numeric; tail numeric;
BEGIN
 IF jsonb_typeof(p_metrics) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_metrics->'p50_ms') IS DISTINCT FROM 'number'
 OR jsonb_typeof(p_metrics->'p95_ms') IS DISTINCT FROM 'number' THEN
 RAISE EXCEPTION 'INVALID_QUALIFICATION_TIMING' USING ERRCODE='22023';END IF;
 median:=(p_metrics->>'p50_ms')::numeric;tail:=(p_metrics->>'p95_ms')::numeric;
 IF median<0 OR tail<median OR tail>9007199254740991 THEN
 RAISE EXCEPTION 'INVALID_QUALIFICATION_TIMING' USING ERRCODE='22023';END IF;
 IF tail=0 THEN RETURN NULL;END IF;
 RETURN ceil(tail)::bigint;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.qualification_timing_ms(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.qualification_timing_ms(jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
-- Preserve existing signature, security checks, immutable replay and ACLs.
-- +goose StatementBegin
DO $$
DECLARE definition text; column_needle text:='trusted_import,status)';
 value_needle text:='true,''passed'') RETURNING id INTO result_id;';
BEGIN
 definition:=pg_get_functiondef('photo.import_v2_qualification(uuid,uuid,uuid,text,uuid,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,column_needle,'')))/length(column_needle)<>1
 OR (length(definition)-length(replace(definition,value_needle,'')))/length(value_needle)<>1 THEN
 RAISE EXCEPTION 'Unexpected qualification timing import definition';END IF;
 definition:=replace(definition,column_needle,'trusted_import,status,measured_p95_ms)');
 definition:=replace(definition,value_needle,'true,''passed'',photo.qualification_timing_ms(p_body->''metrics'')) RETURNING id INTO result_id;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
