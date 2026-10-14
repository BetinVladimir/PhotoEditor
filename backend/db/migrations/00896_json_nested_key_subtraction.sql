-- +goose Up
-- Parenthesize nested JSON extraction before key subtraction. Replace current
-- definitions so subsequent authority/guard fixes and existing ACLs survive.
-- Explicit function allowlist; no table mutation or privilege expansion.
-- +goose StatementBegin
DO $migration$
DECLARE fix record; fn record; definition text; matches integer;
BEGIN
 FOR fix IN SELECT * FROM (VALUES
('materialize_defect_branch','profile->''resource_estimate''-','(profile->''resource_estimate'')-'),
('adopt_ingest_image','p_metadata->''sensor_alignment''-','(p_metadata->''sensor_alignment'')-'),
('semantic_defect_native_parent_ready','native->''original_producer''-','(native->''original_producer'')-'),
('v28_worker_donor_source_ready','c->''params''-','(c->''params'')-'),
('save_v28_donor_descriptor_pre_params_owner','d.v28_donor_issuer_context->''params''-','(d.v28_donor_issuer_context->''params'')-'),
('v28_donor_transport_scope','c->''params''-','(c->''params'')-'),
('v28_donor_params_request_scope','c->''params''-','(c->''params'')-'),
('read_v28_donor_queued_receipt','c->''params''-','(c->''params'')-')
 ) AS corrections(function_name,original,replacement) LOOP
  matches:=0;
  FOR fn IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
   WHERE n.nspname='photo' AND p.proname=fix.function_name LOOP
   definition:=pg_get_functiondef(fn.oid);
   IF strpos(definition,fix.original)>0 THEN
    EXECUTE replace(definition,fix.original,fix.replacement);
    matches:=matches+1;
   END IF;
  END LOOP;
  IF matches<>1 THEN RAISE EXCEPTION 'Expected one JSON guard correction for %; found %',fix.function_name,matches;END IF;
 END LOOP;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
