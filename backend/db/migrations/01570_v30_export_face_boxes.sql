-- +goose Up
-- Readability only. Full native schema, completed producer and original
-- paired input/source qualification remain mandatory in the export reader.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
 old_contracts text:='''DescriptorShard'',''FaceObservations'',''ResolvedStyle'',''Stats20'',''SeriesCorrections'',''PoseSet'',''BodyFit'',''PairAlignment'',''FaceLandmarks'',''AbsoluteLUTArray'',''DustCandidates'',''DefectCandidates'',''DefectReview'',''DefectLabelMap'',''StyleBackgroundAsset''';
 new_contracts text:='''DescriptorShard'',''FaceObservations'',''ResolvedStyle'',''Stats20'',''SeriesCorrections'',''PoseSet'',''BodyFit'',''PairAlignment'',''FaceBoxes'',''FaceLandmarks'',''AbsoluteLUTArray'',''DustCandidates'',''DefectCandidates'',''DefectReview'',''DefectLabelMap'',''StyleBackgroundAsset''';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)',
  'photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF (length(definition)-length(replace(definition,old_contracts,'')))<>length(old_contracts)
  OR strpos(definition,'SECURITY DEFINER')=0 OR strpos(definition,'a.object_key')=0 THEN
   RAISE EXCEPTION 'V30_EXPORT_FACE_BOXES_CATALOG_BASELINE_MISMATCH: %',signature;
  END IF;
  -- No ACL, role, object scope, producer or media predicate is replaced.
  EXECUTE replace(definition,old_contracts,new_contracts);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
