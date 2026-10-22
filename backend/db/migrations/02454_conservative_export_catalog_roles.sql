-- +goose Up
-- Readability only. Typed native schema, original producer, source ancestry and
-- physical PNG/label-map comparison remain separate admission requirements.
-- Existing function owners and grants are retained by CREATE OR REPLACE.
-- +goose StatementBegin
DO $$
DECLARE signature text;definition text;
 roles text:='''DefectLabelMap'',''StyleBackgroundAsset''';
 media text:='WHEN p_contract IN('||quote_literal('EncodedImage')||','||quote_literal('StyleBackgroundAsset')||') THEN';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)',
  'photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)',
  'photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)'
 ] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF definition IS NULL OR strpos(definition,'SECURITY DEFINER')=0
   OR (length(definition)-length(replace(definition,roles,'')))/length(roles)<>1 THEN
   RAISE EXCEPTION 'CONSERVATIVE_EXPORT_CATALOG_ROLE_SHAPE: %',signature;
  END IF;
  definition:=replace(definition,roles,quote_literal('DefectProposeReport')||','||quote_literal('DefectLabelPNG')||','||roles);
  IF signature NOT LIKE 'photo.v30_interchange_export_document(%' THEN
   IF (length(definition)-length(replace(definition,media,'')))/length(media)<>1 THEN
    RAISE EXCEPTION 'CONSERVATIVE_EXPORT_PNG_MEDIA_SHAPE: %',signature;
   END IF;
   definition:=replace(definition,media,'WHEN p_contract='||quote_literal('DefectLabelPNG')||' THEN a.media_type='||quote_literal('image/png')||E'\n   '||media);
  END IF;
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
