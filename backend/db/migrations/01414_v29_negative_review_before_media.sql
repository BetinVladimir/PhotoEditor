-- +goose Up
-- Reuse accepted asset-master before previews with every existing source gate.
-- +goose StatementBegin
DO $$
DECLARE definition text; signature text; needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.v26_review_media_source_ready(uuid)','photo.enqueue_v26_review_before_media(uuid,uuid)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  needle:='v.kind=''render.compose''';
  IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_BEFORE_MEDIA_KIND_PATCH_MISMATCH';END IF;
  EXECUTE replace(definition,needle,'v.kind IN (''render.compose'',''optical.negative'')');
 END LOOP;
 definition:=pg_get_functiondef('photo.create_v29_negative_review(uuid,uuid,uuid,uuid,uuid,text)'::regprocedure);
 needle:='RETURN review_id;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_BEFORE_MEDIA_CREATION_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,'PERFORM photo.enqueue_v26_review_before_media(p_tenant,review_id);'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
