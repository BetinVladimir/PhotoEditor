-- +goose Up
-- A separate media run presents restoration before/after without mutating its
-- waiting-review producer. The foreign key prevents an arbitrary task locator.
ALTER TABLE photo.runs ADD COLUMN v28_restoration_preview_task_id uuid;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v28_restoration_preview_task_fk
 FOREIGN KEY(tenant_id,v28_restoration_preview_task_id)
 REFERENCES photo.tasks(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v28_restoration_preview_kind
 CHECK(v28_restoration_preview_task_id IS NULL OR
  (kind='preview' AND v26_derivative_task_id IS NULL
   AND v29_perspective_preview_task_id IS NULL));
CREATE INDEX runs_v28_restoration_preview_origin ON photo.runs(tenant_id,v28_restoration_preview_task_id)
 WHERE v28_restoration_preview_task_id IS NOT NULL;
CREATE UNIQUE INDEX runs_v28_restoration_preview_identity ON photo.runs(
 tenant_id,v28_restoration_preview_task_id,
 (config_snapshot->>'source_result_id'),(config_snapshot->>'srgb_sha256'))
 WHERE v28_restoration_preview_task_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_restoration_preview_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v28_restoration_preview_task_id IS NOT NULL AND current_user<>'photo_fn_owner'
  THEN RAISE EXCEPTION 'V28 preview requires trusted admission' USING ERRCODE='42501';END IF;
 ELSIF NEW.v28_restoration_preview_task_id IS DISTINCT FROM OLD.v28_restoration_preview_task_id OR
  (OLD.v28_restoration_preview_task_id IS NOT NULL AND
   ROW(NEW.id,NEW.tenant_id,NEW.kind,NEW.created_by,NEW.input_snapshot_id,
    NEW.config_snapshot,NEW.config_hash) IS DISTINCT FROM
   ROW(OLD.id,OLD.tenant_id,OLD.kind,OLD.created_by,OLD.input_snapshot_id,
    OLD.config_snapshot,OLD.config_hash))
 THEN RAISE EXCEPTION 'V28 preview intent is immutable' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v28_restoration_preview_intent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_restoration_preview_intent() FROM PUBLIC;
CREATE TRIGGER runs_v28_restoration_preview_intent_guard BEFORE INSERT OR UPDATE
 ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v28_restoration_preview_intent();

-- +goose StatementBegin
CREATE FUNCTION photo.next_v28_restoration_preview_producer(p_after uuid,p_srgb text)
RETURNS TABLE(tenant_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs parent ON parent.tenant_id=t.tenant_id AND parent.id=t.run_id
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id
  AND result.id=t.canonical_result_id AND result.task_id=t.id
 JOIN photo.reviews review ON review.tenant_id=t.tenant_id AND review.task_id=t.id
  AND review.run_id=t.run_id AND review.kind=t.operation AND review.state='pending'
 WHERE p_srgb ~ '^[0-9a-f]{64}$' AND (p_after IS NULL OR t.id>p_after)
  AND t.operation IN('portrait.glare','image.reflection')
  AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
  AND t.canonical_result_id IS NOT NULL AND result.review_required
  AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
  AND parent.kind='v2' AND parent.graph_sealed AND NOT parent.cancel_requested
  AND parent.status='waiting_review'
  AND parent.v28_restoration_adoption_receipt->>'task'=t.id::text
  AND parent.v28_restoration_adoption_receipt->>'result'=result.id::text
  AND parent.v28_restoration_adoption_receipt->'evidence'->'candidate'->'image'=
   jsonb_build_object('artifact_id',review.candidate_artifact_id,'sha256',review.candidate_hash)
  AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
   AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,result.id))
  AND NOT EXISTS(SELECT 1 FROM photo.runs preview WHERE preview.tenant_id=t.tenant_id
   AND preview.v28_restoration_preview_task_id=t.id
   AND preview.config_snapshot->>'source_result_id'=result.id::text
   AND preview.config_snapshot->>'srgb_sha256'=p_srgb
   AND (preview.graph_sealed OR preview.cancel_requested OR preview.status<>'draft'))
 ORDER BY t.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v28_restoration_preview_producer(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v28_restoration_preview_producer(uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.next_v28_restoration_preview_producer(uuid,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
