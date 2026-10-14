-- +goose Up
-- Reuse the fenced review media job for the exact accepted donor base version.
-- The existing claim, materialization and publication guards recheck this
-- source function, so a changed donor binding cancels publication.
-- +goose StatementBegin
DO $$ DECLARE definition text;signature text;needle text;replacement text;
BEGIN
 needle:='v.kind IN (''render.compose'',''optical.negative'')';
 replacement:='v.kind IN (''render.compose'',''optical.negative'',''portrait.open_eyes'')'
  ||' AND (v.kind<>''portrait.open_eyes'' OR ('
  ||'version.id::text=parent.v28_donor_descriptor_jcs::jsonb->''params''->''base''->>''asset_version_id'''
  ||' AND version.master_artifact_id::text=parent.v28_donor_descriptor_jcs::jsonb->''params''->''base''->''source''->>''artifact_id'''
  ||' AND version.content_hash=parent.v28_donor_descriptor_jcs::jsonb->''params''->''base''->''source''->>''sha256''))';
 FOREACH signature IN ARRAY ARRAY['photo.v26_review_media_source_ready(uuid)',
  'photo.enqueue_v26_review_before_media(uuid,uuid)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'OPEN_EYES_BEFORE_MEDIA_KIND_PATCH_MISMATCH';END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
 definition:=pg_get_functiondef('photo.create_v28_open_eyes_review(uuid,uuid,uuid)'::regprocedure);
 needle:='RETURN review_id;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'OPEN_EYES_BEFORE_MEDIA_CREATION_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,
  'PERFORM photo.enqueue_v26_review_before_media(p_tenant,review_id);'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
