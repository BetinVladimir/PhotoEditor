-- +goose Up
-- Preserve canonical native array and image media after successful export.
-- Replace only the media predicate; retain result/input membership and ACLs.
-- +goose StatementBegin
DO $$
DECLARE signature text:='photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)';
 definition text;
 old_clause text:=$old$AND a.media_type=CASE WHEN p_contract='EncodedImage' THEN a.media_type
 WHEN p_contract IN('WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray','DenseWarpField','DenseWarpArray') THEN 'application/octet-stream' ELSE 'application/json' END
 AND (p_contract<>'EncodedImage' OR a.media_type IN('image/jpeg','image/png','image/tiff'))$old$;
 new_clause text:=$new$AND CASE
 WHEN p_contract IN('EncodedImage','StyleBackgroundAsset') THEN a.media_type IN('image/jpeg','image/png','image/tiff')
 WHEN p_contract IN('WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray','DenseWarpField','DenseWarpArray') THEN a.media_type IN('application/octet-stream','application/x-npy')
 ELSE a.media_type='application/json' END$new$;
BEGIN
 definition:=pg_get_functiondef(signature::regprocedure);
 IF (length(definition)-length(replace(definition,old_clause,'')))<>length(old_clause)
 OR strpos(definition,'SECURITY DEFINER')=0 OR strpos(definition,'a.object_key')=0
 OR strpos(definition,'photo.v30_interchange_export_output')=0
 OR strpos(definition,'i.contract=p_contract')=0 THEN
  RAISE EXCEPTION 'V30_EXPORT_RESULT_MEDIA_BASELINE_MISMATCH';
 END IF;
 EXECUTE replace(definition,old_clause,new_clause);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
