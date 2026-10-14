-- +goose Up
-- Repair immutable storage key reads; preserve function OIDs, owner and ACLs.
-- The latest installed export functions used a.key, absent from artifacts.
-- +goose StatementBegin
DO $$
DECLARE item record;definition text;occurrences integer;
BEGIN
 FOR item IN SELECT * FROM (VALUES
 ('photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)',3),
 ('photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text)',1),
 ('photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)',1)
 ) AS targets(signature,expected_count) LOOP
  definition:=pg_get_functiondef(item.signature::regprocedure);
  occurrences:=(length(definition)-length(replace(definition,'a.key','')))/length('a.key');
  IF occurrences<>item.expected_count OR strpos(definition,'photo.artifacts a')=0
   OR strpos(definition,'SECURITY DEFINER')=0 THEN
   RAISE EXCEPTION 'V30_EXPORT_OBJECT_KEY_BASELINE_MISMATCH: %',item.signature;
  END IF;
  EXECUTE replace(definition,'a.key','a.object_key');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
