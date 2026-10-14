-- +goose Up
-- Readability only: JSON and binary semantic/producer gates remain mandatory.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
 old_contracts text:='''DescriptorShard'',''FaceObservations'',''StyleBackgroundAsset''';
 new_contracts text:='''DescriptorShard'',''FaceObservations'',''ResolvedStyle'',''DustCandidates'',''DefectCandidates'',''DefectReview'',''DefectLabelMap'',''StyleBackgroundAsset''';
 old_arrays text:='''MaskArray'',''DenseWarpField'',''DenseWarpArray'') THEN a.media_type';
 new_arrays text:='''MaskArray'',''DenseWarpField'',''DenseWarpArray'',''DefectLabelMap'') THEN a.media_type';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)',
  'photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,old_contracts,'')))<>length(old_contracts)
  OR strpos(definition,'SECURITY DEFINER')=0 OR strpos(definition,'a.object_key')=0 THEN
   RAISE EXCEPTION 'V30_EXPORT_REPAIR_CATALOG_BASELINE_MISMATCH: %',signature;
  END IF;
  definition:=replace(definition,old_contracts,new_contracts);
  IF strpos(signature,'result_input')>0 THEN
   IF (length(definition)-length(replace(definition,old_arrays,'')))<>length(old_arrays) THEN
    RAISE EXCEPTION 'V30_EXPORT_REPAIR_MEDIA_BASELINE_MISMATCH';
   END IF;
   definition:=replace(definition,old_arrays,new_arrays);
  END IF;
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
