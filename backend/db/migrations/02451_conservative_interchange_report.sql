-- +goose Up
-- Add one JSON dependency role. Producer/physical authority is independently
-- required by Go; this function only pins existing accepted import receipts.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 anchor text := '''DefectCandidates'',''DefectReview'',''DustCandidates''';
BEGIN
 SELECT pg_get_functiondef('photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'CONSERVATIVE_REPORT_PREPARED_SCOPE_SHAPE';
 END IF;
 definition:=replace(definition,anchor,anchor||','||quote_literal('DefectProposeReport'));
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
