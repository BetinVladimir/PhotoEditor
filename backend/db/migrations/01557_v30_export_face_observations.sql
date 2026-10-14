-- +goose Up
-- FaceObservations is a required typed DescriptorShard dependency. This grants
-- immutable byte readability only; contextual frame qualification stays required.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;needle text:='''CandidateProfile'',''DescriptorShard'',''StyleBackgroundAsset''';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)',
  'photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,needle,'')))<>length(needle)
   OR strpos(definition,'SECURITY DEFINER')=0 OR strpos(definition,'a.object_key')=0 THEN
   RAISE EXCEPTION 'V30_EXPORT_FACE_CATALOG_BASELINE_MISMATCH: %',signature;
  END IF;
  EXECUTE replace(definition,needle,'''CandidateProfile'',''DescriptorShard'',''FaceObservations'',''StyleBackgroundAsset''');
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
