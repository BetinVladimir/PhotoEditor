-- Freeze must bind the baseline to the profile state at the same boundary as
-- the candidate inventory and consents. A stale active or missing zero model
-- must never enter a sealed training snapshot.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v23_training_baseline_freeze() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; p photo.learning_profiles%ROWTYPE;
 recovery jsonb; manifest jsonb; baseline jsonb;
 expected_sha text; expected_id uuid; h bytea;
BEGIN
 IF NEW.payload->>'contract' IS DISTINCT FROM 'TrainingManifest' THEN
  RETURN NEW; END IF;
 SELECT x.* INTO r FROM photo.runs x WHERE x.tenant_id=NEW.tenant_id
  AND x.input_snapshot_id=NEW.snapshot_id
  AND x.config_snapshot ? 'v23_training';
 IF r.id IS NULL THEN RETURN NEW; END IF;
 IF r.status<>'draft' OR r.graph_sealed OR r.created_by IS NULL OR
  NEW.sha256 IS DISTINCT FROM
   r.config_snapshot->'v23_training'->>'manifest_sha' OR
  NEW.payload->>'owner_id' IS DISTINCT FROM
   r.config_snapshot->'v23_training'->>'profile_id' THEN
  RAISE EXCEPTION 'V23_TRAIN_BASELINE_FREEZE_CONFLICT'
   USING ERRCODE='40001';
 END IF;
 SELECT x.* INTO p FROM photo.learning_profiles x
  WHERE x.tenant_id=NEW.tenant_id AND x.id::text=
   r.config_snapshot->'v23_training'->>'profile_id';
 SELECT i.response_body INTO recovery FROM photo.idempotency_records i
  WHERE i.tenant_id=NEW.tenant_id AND i.principal_type='user'
   AND i.principal_id=r.created_by AND i.route='V2.Admit'
   AND i.resource_id=r.id AND i.response_status IS NULL;
 IF p.id IS NULL OR recovery IS NULL OR
  recovery->>'manifest_sha' IS DISTINCT FROM NEW.sha256 OR
  jsonb_typeof(recovery->'manifest_jcs') IS DISTINCT FROM 'string' THEN
  RAISE EXCEPTION 'V23_TRAIN_BASELINE_FREEZE_CONFLICT'
   USING ERRCODE='40001'; END IF;
 manifest:=(recovery->>'manifest_jcs')::jsonb;
 baseline:=manifest->'baseline';
 expected_sha:=baseline->>'sha256';
 IF coalesce(expected_sha,'') !~ '^[0-9a-f]{64}$' OR
  coalesce(baseline->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  THEN RAISE EXCEPTION 'V23_TRAIN_BASELINE_FREEZE_CONFLICT'
   USING ERRCODE='40001'; END IF;
 expected_id:=(baseline->>'artifact_id')::uuid;
 IF p.active_version_id IS NULL THEN
  h:=public.digest(uuid_send(r.id)||convert_to(
   'v23-zero-profile/'||p.id::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);
  h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  IF expected_id IS DISTINCT FROM
   encode(substring(h FROM 1 FOR 16),'hex')::uuid OR
   NOT EXISTS(SELECT 1 FROM photo.backend_documents d
    JOIN photo.artifacts a ON a.tenant_id=d.tenant_id
     AND a.id=d.artifact_id AND a.sha256=d.sha256
     AND a.object_version=d.object_version
    WHERE d.tenant_id=NEW.tenant_id AND d.run_id=r.id
     AND d.owner_id=p.id AND d.purpose='v23_zero_profile'
     AND d.artifact_id=expected_id AND d.sha256=expected_sha
     AND d.state='ready' AND a.state='ready'
     AND a.verified_at IS NOT NULL
     AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
   THEN RAISE EXCEPTION 'V23_TRAIN_BASELINE_FREEZE_CONFLICT'
    USING ERRCODE='40001'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM photo.learning_versions v
   JOIN photo.version_eligibility e ON e.tenant_id=v.tenant_id
    AND e.project_id=v.project_id AND e.version_id=v.id
    AND e.status='eligible'
   JOIN photo.artifacts a ON a.tenant_id=v.tenant_id
    AND a.id=v.model_artifact_id
   JOIN photo.artifacts training ON training.tenant_id=v.tenant_id
    AND training.id=v.training_artifact_id
   WHERE v.tenant_id=NEW.tenant_id AND v.project_id=p.project_id
    AND v.profile_id=p.id AND v.id=p.active_version_id
    AND a.id=expected_id AND a.sha256=expected_sha
    AND a.state='ready' AND a.media_type='application/json'
    AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
    AND a.object_version NOT IN ('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND training.state='ready' AND training.verified_at IS NOT NULL
    AND training.object_version IS NOT NULL
    AND training.object_version NOT IN ('','null')
    AND (training.expires_at IS NULL OR
     training.expires_at>clock_timestamp()))
   THEN RAISE EXCEPTION 'V23_TRAIN_BASELINE_FREEZE_CONFLICT'
    USING ERRCODE='40001'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v23_training_baseline_freeze() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v23_training_baseline_freeze() FROM PUBLIC;
CREATE TRIGGER v23_training_baseline_freeze_guard
 BEFORE INSERT ON photo.snapshot_items FOR EACH ROW
 EXECUTE FUNCTION photo.guard_v23_training_baseline_freeze();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
