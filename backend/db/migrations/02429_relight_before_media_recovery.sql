-- +goose Up
-- Extend the existing fenced review-media compiler, retaining donor guards.
-- +goose StatementBegin
DO $$ DECLARE definition text;signature text;needle text;replacement text;
BEGIN
 needle:='v.kind IN (''render.compose'',''optical.negative'',''portrait.open_eyes'')';
 replacement:='v.kind IN (''render.compose'',''optical.negative'',''portrait.open_eyes'',''image.relight'')'
  ||' AND (v.kind<>''image.relight'' OR ('
  ||'version.id::text=v.relight_params_jcs::jsonb->''base''->>''asset_version_id'''
  ||' AND version.master_artifact_id::text=v.relight_params_jcs::jsonb->''base''->''source''->>''artifact_id'''
  ||' AND version.content_hash=v.relight_params_jcs::jsonb->''base''->''source''->>''sha256''))';
 FOREACH signature IN ARRAY ARRAY['photo.v26_review_media_source_ready(uuid)',
  'photo.enqueue_v26_review_before_media(uuid,uuid)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'RELIGHT_BEFORE_MEDIA_KIND_PATCH_MISMATCH';END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
 definition:=pg_get_functiondef('photo.create_v28_relight_review(uuid,uuid,uuid,text)'::regprocedure);
 needle:='RETURN review_id;';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'RELIGHT_BEFORE_MEDIA_CREATION_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,
  'PERFORM photo.enqueue_v26_review_before_media(p_tenant,review_id);'||needle);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_next_v28_relight_before_media()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record;event_id uuid;
BEGIN
 SELECT review.tenant_id,review.id INTO selected FROM photo.reviews review
 JOIN photo.tenants tenant ON tenant.id=review.tenant_id AND tenant.status='active'
 JOIN photo.runs parent ON parent.tenant_id=review.tenant_id AND parent.id=review.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.asset_versions version ON version.tenant_id=review.tenant_id
  AND version.id=CASE WHEN review.relight_params_jcs::jsonb->'base'->>'asset_version_id'
   ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   THEN (review.relight_params_jcs::jsonb->'base'->>'asset_version_id')::uuid END
  AND version.master_artifact_id=review.base_artifact_id
  AND version.content_hash=review.relight_params_jcs::jsonb->'base'->'source'->>'sha256'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 WHERE review.kind='image.relight' AND review.state='pending'
  AND parent.kind='v2' AND parent.status='waiting_review' AND parent.graph_sealed
  AND NOT parent.cancel_requested AND version.review_state='accepted'
  AND version.created_by_run_id IS NOT NULL
  AND asset.project_id=workflow.project_id AND asset.deleted_at IS NULL
  AND asset.upload_state='verified'
  AND NOT EXISTS(SELECT 1 FROM photo.media_retry_jobs job
   WHERE job.tenant_id=review.tenant_id AND job.review_id=review.id
    AND job.asset_version_id=version.id AND job.origin='review')
 ORDER BY review.created_at,review.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 event_id:=photo.enqueue_v26_review_before_media(selected.tenant_id,selected.id);
 RETURN event_id IS NOT NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_next_v28_relight_before_media() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_next_v28_relight_before_media() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_next_v28_relight_before_media() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
