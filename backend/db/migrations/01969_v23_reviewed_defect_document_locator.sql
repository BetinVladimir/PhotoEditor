-- Exact-version reader for the event-owned reviewed holdout document.
-- The caller must repeat this lookup after object IO and compare every field.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_defect_document_locator(
 p_tenant uuid,p_run uuid,p_event uuid,p_review uuid,p_actor uuid,
 p_auth_session uuid,p_decision_hash text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,
 sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE;
 h bytea; expected_id uuid; profile_id uuid; manifest_sha text;
 project_id uuid; event_source record; review_source record;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_review IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_artifact IS NULL OR p_decision_hash IS NULL OR
  p_decision_hash !~ '^[0-9a-f]{64}$' OR p_sha IS NULL OR
  p_sha !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-defect/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact<>expected_id THEN RETURN; END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND
  x.id=p_run AND x.kind='v2' AND
  NOT x.cancel_requested AND x.created_by=p_actor;
 IF NOT FOUND THEN RETURN; END IF;
 profile_id:=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 manifest_sha:=r.config_snapshot->'v23_training'->>'manifest_sha';
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p
  WHERE p.tenant_id=p_tenant AND p.id=profile_id;
 IF project_id IS NULL OR manifest_sha IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.snapshots s
   JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND
    i.snapshot_id=s.id AND i.ordinal=0
   JOIN photo.backend_documents m ON m.tenant_id=i.tenant_id AND
    m.artifact_id=i.artifact_id AND m.sha256=i.sha256
   JOIN photo.artifacts a ON a.tenant_id=m.tenant_id AND
    a.id=m.artifact_id AND a.sha256=m.sha256 AND
    a.object_version=m.object_version AND a.state='ready'
   WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id AND
    s.kind='v2' AND s.expires_at IS NULL AND s.item_count=1 AND
    i.sha256=manifest_sha AND i.payload=jsonb_build_object(
     'contract','TrainingManifest','owner_id',profile_id) AND
    m.run_id=p_run AND m.owner_id=profile_id AND
    m.purpose='v23_training_manifest' AND m.state='ready')
  THEN RETURN; END IF;
 SELECT * INTO event_source FROM photo.v23_reviewed_defect_event_source(
  p_tenant,project_id,p_actor,p_auth_session,p_event);
 IF NOT FOUND OR event_source.review_id IS DISTINCT FROM p_review THEN
  RETURN; END IF;
 SELECT * INTO review_source FROM photo.v23_reviewed_defect_source(
  p_tenant,project_id,p_actor,p_auth_session,p_review);
 IF NOT FOUND OR review_source.decision_hash IS DISTINCT FROM
  p_decision_hash OR review_source.image_id IS DISTINCT FROM
  event_source.source_id THEN RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_event AND
  x.purpose='v23_defect_holdout' AND x.artifact_id=p_artifact AND
  x.sha256=p_sha AND x.state='ready' AND x.object_version IS NOT NULL;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,
  a.sha256,a.bytes,a.media_type FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
   AND a.bucket=d.bucket AND a.object_version=d.object_version
   AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||
    '/documents/'||p_artifact AND a.output_role='v23_defect_holdout'
   AND a.media_type='application/json' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.bytes=d.bytes
   AND a.bytes BETWEEN 2 AND 1048576
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_defect_document_locator(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_defect_document_locator(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_reviewed_defect_document_locator(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
