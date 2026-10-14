-- +goose Up
-- Separate immutable request identity, same existing fenced media compiler.
ALTER TABLE photo.media_retry_jobs ADD COLUMN review_id uuid;
ALTER TABLE photo.media_retry_jobs ADD CONSTRAINT media_review_fk FOREIGN KEY(tenant_id,review_id)
 REFERENCES photo.reviews(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.media_retry_jobs DROP CONSTRAINT media_job_origin_ck;
ALTER TABLE photo.media_retry_jobs ADD CONSTRAINT media_job_origin_ck CHECK(
 (origin='bulk_repair' AND bulk_operation_id IS NOT NULL AND item_id IS NOT NULL AND origin_run_id IS NULL AND review_id IS NULL)
 OR (origin IN('ingest','apply') AND bulk_operation_id IS NULL AND item_id IS NULL AND origin_run_id IS NOT NULL AND review_id IS NULL)
 OR (origin='review' AND bulk_operation_id IS NULL AND item_id IS NULL AND origin_run_id IS NOT NULL AND review_id IS NOT NULL));
CREATE UNIQUE INDEX media_review_job_once ON photo.media_retry_jobs(tenant_id,review_id,asset_version_id) WHERE origin='review';
-- +goose StatementBegin
CREATE FUNCTION photo.v26_review_media_source_ready(p_event uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.media_retry_jobs j
 JOIN photo.tenants tenant ON tenant.id=j.tenant_id AND tenant.status='active'
 JOIN photo.reviews v ON v.tenant_id=j.tenant_id AND v.id=j.review_id
 JOIN photo.runs parent ON parent.tenant_id=v.tenant_id AND parent.id=v.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.asset_versions version ON version.tenant_id=j.tenant_id AND version.id=j.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.runs source ON source.tenant_id=j.tenant_id AND source.id=j.origin_run_id
 JOIN photo.artifacts master ON master.tenant_id=version.tenant_id AND master.id=version.master_artifact_id AND master.sha256=version.content_hash
 WHERE j.event_id=p_event AND j.origin='review' AND v.kind='render.compose' AND v.state='pending'
 AND parent.status='waiting_review' AND parent.graph_sealed AND NOT parent.cancel_requested
 AND version.master_artifact_id=v.base_artifact_id AND version.review_state='accepted' AND version.created_by_run_id=source.id
 AND asset.project_id=workflow.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.kind IN('ingest','apply','v2') AND NOT source.cancel_requested
 AND master.output_role='working_image' AND master.media_type='application/json' AND master.state='ready' AND master.verified_at IS NOT NULL
 AND coalesce(master.object_version,'') NOT IN('','null') AND (master.expires_at IS NULL OR master.expires_at>clock_timestamp()));
$$;
ALTER FUNCTION photo.v26_review_media_source_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_review_media_source_ready(uuid) FROM PUBLIC;
CREATE FUNCTION photo.enqueue_v26_review_before_media(p_tenant uuid,p_review uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record; queued_event uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT version.id,version.created_by_run_id INTO selected FROM photo.reviews v
 JOIN photo.runs parent ON parent.tenant_id=v.tenant_id AND parent.id=v.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=parent.tenant_id AND w.run_id=parent.id
 JOIN photo.asset_versions version ON version.tenant_id=v.tenant_id AND version.master_artifact_id=v.base_artifact_id
 JOIN photo.assets a ON a.tenant_id=version.tenant_id AND a.id=version.asset_id AND a.project_id=w.project_id
 WHERE v.tenant_id=p_tenant AND v.id=p_review AND v.kind='render.compose' AND v.state='pending'
 AND parent.status='waiting_review' AND NOT parent.cancel_requested AND version.review_state='accepted'
 AND a.deleted_at IS NULL AND a.upload_state='verified' AND version.created_by_run_id IS NOT NULL
 ORDER BY version.id LIMIT 1;
 IF NOT FOUND THEN RETURN NULL;END IF;
 queued_event:=photo.v30_uuid5(p_review,'review:before:media:'||selected.id::text);
 INSERT INTO photo.media_retry_jobs(event_id,tenant_id,asset_version_id,variants,origin,origin_run_id,review_id)
 VALUES(queued_event,p_tenant,selected.id,'["preview"]','review',selected.created_by_run_id,p_review)
 ON CONFLICT(tenant_id,review_id,asset_version_id) WHERE origin='review' DO NOTHING;
 IF NOT photo.v26_review_media_source_ready(queued_event) THEN
 UPDATE photo.media_retry_jobs SET status='cancelled',error_code='SOURCE_CHANGED' WHERE media_retry_jobs.event_id=queued_event AND status='queued';
 RETURN NULL;END IF;
 RETURN queued_event;
END $$;
ALTER FUNCTION photo.enqueue_v26_review_before_media(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_v26_review_before_media(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_v26_review_before_media(uuid,uuid) TO photo_scheduler;
-- Preserve original function identities and all existing origins.
DO $$
DECLARE body text; needle text;
BEGIN
 body:=pg_get_functiondef('photo.claim_media_retry_job()'::regprocedure);
 needle:='IF NOT FOUND THEN RETURN;END IF;';
 IF strpos(body,needle)=0 THEN RAISE EXCEPTION 'REVIEW_MEDIA_CLAIM_PATCH_MISMATCH';END IF;
 body:=replace(body,needle,needle||$patch$
 IF j.origin='review' AND NOT photo.v26_review_media_source_ready(j.event_id) THEN
 UPDATE photo.media_retry_jobs AS target SET status='cancelled',error_code='SOURCE_CHANGED',materialize_token=NULL,materialize_lease_until=NULL WHERE target.event_id=j.event_id;RETURN;END IF;
$patch$);
 EXECUTE body;
 body:=pg_get_functiondef('photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb)'::regprocedure);
 needle:='source.kind<>j.origin';
 IF strpos(body,needle)=0 THEN RAISE EXCEPTION 'REVIEW_MEDIA_PLAN_PATCH_MISMATCH';END IF;
 body:=replace(body,needle,$patch$((j.origin='review' AND (source.kind NOT IN('ingest','apply','v2') OR NOT photo.v26_review_media_source_ready(j.event_id))) OR (j.origin<>'review' AND source.kind<>j.origin))$patch$);
 EXECUTE body;
 body:=pg_get_functiondef('photo.create_v26_render_review(uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 needle:='RETURN review_id;';
 IF strpos(body,needle)=0 THEN RAISE EXCEPTION 'REVIEW_MEDIA_CREATION_PATCH_MISMATCH';END IF;
 EXECUTE replace(body,needle,'PERFORM photo.enqueue_v26_review_before_media(p_tenant,review_id);'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
