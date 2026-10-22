-- +goose Up
-- Optional failed exports stay pending until assembly adoption writes omitted.
-- The signed receipt reader must accept that pre-commit state; the subsequent
-- current-matrix predicate still proves every pending slot is an authorized
-- optional omission against the live export observation.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 old_predicate text := 'state NOT IN(''ready'',''omitted'')';
 new_predicate text := '(state NOT IN(''ready'',''pending'',''omitted'') OR (state IN(''pending'',''omitted'') AND required))';
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text)'::regprocedure)
 INTO definition;
 IF definition IS NULL OR position(old_predicate IN definition)=0
  OR position(new_predicate IN definition)>0
  OR position('ready_count<>jsonb_array_length(closure->''entries'')' IN definition)=0
  OR position('photo.v30_delivery_signed_runtime_documents(' IN definition)=0
  OR position('FROM photo.delivery_items' IN definition)=0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNED_OPTIONAL_OMISSION_SOURCE_SHAPE';
 END IF;
 definition:=replace(definition,old_predicate,new_predicate);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
