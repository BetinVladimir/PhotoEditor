-- +goose Up
-- Both direct and post-derivative encoders produce clean intermediates.
-- Preserve all existing source, lease, quota and ownership predicates.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['photo.v30_delivery_params_scope_initial(uuid,uuid,integer,text)','photo.v30_delivery_params_scope(uuid,uuid,integer,text)'] LOOP
  definition:=pg_get_functiondef(target::regprocedure);
  needle:='''icc_sha256'',params->>''icc_sha256'')';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'METADATA_ENCODE_PARAMS_SCOPE_DEFINITION_DRIFT: %',target;
  END IF;
  EXECUTE replace(definition,needle,'''icc_sha256'',params->>''icc_sha256'',''metadata_policy'',''strip'')');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
