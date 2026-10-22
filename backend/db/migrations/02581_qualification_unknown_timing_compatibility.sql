-- +goose Up
-- NULL denotes historical unknown timing, including failed/untrusted records.
-- Non-NULL timing remains restricted to trusted passed qualifications.
-- +goose StatementBegin
DO $$
DECLARE target record; old_expression text; new_expression text;
BEGIN
 CREATE TEMP TABLE qualification_timing_expected_old(measured_p95_ms bigint,trusted_import boolean,status text,
  CHECK(measured_p95_ms BETWEEN 1 AND 9007199254740991 AND trusted_import AND status='passed')) ON COMMIT DROP;
 CREATE TEMP TABLE qualification_timing_expected_new(measured_p95_ms bigint,trusted_import boolean,status text,
  CHECK(measured_p95_ms IS NULL OR (measured_p95_ms BETWEEN 1 AND 9007199254740991 AND trusted_import AND status='passed'))) ON COMMIT DROP;
 SELECT pg_get_expr(conbin,conrelid) INTO old_expression FROM pg_constraint WHERE conrelid='pg_temp.qualification_timing_expected_old'::regclass AND contype='c';
 SELECT pg_get_expr(conbin,conrelid) INTO new_expression FROM pg_constraint WHERE conrelid='pg_temp.qualification_timing_expected_new'::regclass AND contype='c';
 IF (SELECT count(*) FROM pg_constraint c WHERE c.conrelid='photo.model_qualifications'::regclass AND c.contype='c'
  AND (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='measured_p95_ms')=ANY(c.conkey))<>1 THEN
  RAISE EXCEPTION 'QUALIFICATION_TIMING_CONSTRAINT_SOURCE_SHAPE'; END IF;
 SELECT c.conname,pg_get_expr(c.conbin,c.conrelid) expression INTO STRICT target FROM pg_constraint c
 WHERE c.conrelid='photo.model_qualifications'::regclass AND c.contype='c'
 AND (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='measured_p95_ms')=ANY(c.conkey);
 IF target.expression NOT IN(old_expression,new_expression) THEN
  RAISE EXCEPTION 'QUALIFICATION_TIMING_CONSTRAINT_SOURCE_SHAPE'; END IF;
 IF target.expression=old_expression THEN
  EXECUTE format('ALTER TABLE photo.model_qualifications DROP CONSTRAINT %I',target.conname);
  EXECUTE format('ALTER TABLE photo.model_qualifications ADD CONSTRAINT %I CHECK(measured_p95_ms IS NULL OR (measured_p95_ms BETWEEN 1 AND 9007199254740991 AND trusted_import AND status=''passed''))',target.conname);
 END IF;
 DROP TABLE pg_temp.qualification_timing_expected_old,pg_temp.qualification_timing_expected_new;
END $$;
-- +goose StatementEnd
-- +goose Down
-- Forward-only compatibility repair.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
