-- V23 freeze: only a complete, current training inventory may become the
-- immutable input snapshot of a learning run.
-- +goose Up
GRANT EXECUTE ON FUNCTION photo.v23_training_candidate_rows(uuid,uuid,bigint,uuid[],uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.freeze_v23_training_snapshot(p_tenant uuid,p_run uuid,
 p_profile uuid,p_actor uuid,p_auth_session uuid,p_expected bigint,
 p_manifest jsonb,p_manifest_sha text,p_snapshot_body_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; s photo.snapshots%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; profile_row photo.learning_profiles%ROWTYPE;
 manifest_doc photo.backend_documents%ROWTYPE;
 root_doc photo.backend_documents%ROWTYPE;
 sessions uuid[]; example_count integer; matched_count integer;
 consent_count integer; used_count integer; grant_item jsonb;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_profile IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR p_expected IS NULL OR
  p_expected<1 OR p_manifest_sha IS NULL OR
  p_manifest_sha !~ '^[0-9a-f]{64}$' OR
  p_snapshot_body_sha IS NULL OR
  p_snapshot_body_sha !~ '^[0-9a-f]{64}$' OR
  jsonb_typeof(p_manifest) IS DISTINCT FROM 'object' OR
  p_manifest->>'schema_version' IS DISTINCT FROM '1' OR
  p_manifest->>'profile_id' IS DISTINCT FROM p_profile::text OR
  jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array' OR
  jsonb_typeof(p_manifest->'consents') IS DISTINCT FROM 'array' OR
  octet_length(p_manifest::text)>8388608 THEN RETURN false; END IF;
 example_count:=jsonb_array_length(p_manifest->'examples');
 consent_count:=jsonb_array_length(p_manifest->'consents');
 IF example_count NOT BETWEEN 40 AND 5000 OR
  consent_count NOT BETWEEN 1 AND 5000 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO profile_row FROM photo.learning_profiles p WHERE p.tenant_id=p_tenant
  AND p.id=p_profile FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run
  FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run;
 IF profile_row.id IS NULL OR profile_row.revision<>p_expected OR
  r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR
  r.graph_sealed OR r.cancel_requested OR r.created_by<>p_actor OR
  r.config_snapshot->'v23_training'->>'profile_id'<>p_profile::text OR
  r.config_snapshot->'v23_training'->>'expected_revision'<>p_expected::text OR
  r.config_snapshot->'v23_training'->>'manifest_sha'<>p_manifest_sha OR
  r.config_snapshot->'v23_training'->>'snapshot_body_sha'<>p_snapshot_body_sha OR
  w.id IS NULL OR w.workflow_type<>'learning' OR w.renderer_version<>'v2.2' OR
  w.project_id<>profile_row.project_id OR w.input_snapshot_id<>r.input_snapshot_id OR
  NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_auth_session) OR
  NOT EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
    AND q.state='reserved' AND q.expires_at>clock_timestamp()) THEN
  RETURN false;
 END IF;
 SELECT * INTO s FROM photo.snapshots x WHERE x.tenant_id=p_tenant
  AND x.id=r.input_snapshot_id FOR UPDATE;
 IF s.id IS NULL OR s.kind<>'v2' OR s.principal_type<>'user' OR
  s.principal_id<>p_actor OR s.item_count<>1 OR
  s.digest<>r.config_snapshot->'v23_training'->>'snapshot_digest' THEN
  RETURN false; END IF;
 SELECT * INTO manifest_doc FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_profile
   AND d.purpose='v23_training_manifest' AND d.state='ready'
   AND d.sha256=p_manifest_sha FOR SHARE;
 SELECT * INTO root_doc FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_run
   AND d.purpose='v23_training_snapshot' AND d.state='ready'
   AND d.sha256=p_snapshot_body_sha FOR SHARE;
 IF manifest_doc.id IS NULL OR root_doc.id IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=manifest_doc.artifact_id AND a.sha256=p_manifest_sha
   AND a.object_version=manifest_doc.object_version AND a.state='ready'
   AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR
    a.expires_at>clock_timestamp())) OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=root_doc.artifact_id AND a.sha256=p_snapshot_body_sha
   AND a.object_version=root_doc.object_version AND a.state='ready'
   AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR
    a.expires_at>clock_timestamp())) THEN RETURN false; END IF;
 IF s.expires_at IS NULL THEN
  RETURN s.artifact_id=root_doc.artifact_id AND EXISTS(
   SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
    AND i.snapshot_id=s.id AND i.ordinal=0 AND i.item_id IS NULL
    AND i.asset_version_id IS NULL AND i.artifact_id=manifest_doc.artifact_id
    AND i.sha256=p_manifest_sha AND i.payload=jsonb_build_object(
     'contract','TrainingManifest','owner_id',p_profile));
 END IF;
 IF s.expires_at<=clock_timestamp() OR s.artifact_id IS NOT NULL OR
  EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
   AND i.snapshot_id=s.id) OR
  p_manifest->>'policy_sha' IS DISTINCT FROM w.policy_sha OR
  p_manifest->>'mode' IS DISTINCT FROM profile_row.mode OR
  p_manifest->>'person_id' IS DISTINCT FROM profile_row.person_id::text THEN
  RETURN false; END IF;
 SELECT array_agg(DISTINCT (e.value->>'session_id')::uuid ORDER BY
  (e.value->>'session_id')::uuid) INTO sessions
 FROM jsonb_array_elements(p_manifest->'examples') e;
 IF cardinality(sessions) NOT BETWEEN 6 AND 5000 THEN RETURN false; END IF;
 -- The source function repeats project ACL, revision, selected sessions and
 -- consent/artifact readiness under the same tenant/profile locks.
 WITH candidates AS MATERIALIZED (
  SELECT * FROM photo.v23_training_candidate_rows(p_tenant,p_profile,
   p_expected,sessions,p_actor,p_auth_session)
 ), examples AS (
  SELECT value AS body FROM jsonb_array_elements(p_manifest->'examples')
 ), matched AS (
  SELECT c.event_id,c.consent_id,c.consent_epoch
  FROM examples e JOIN candidates c ON c.event_id=(e.body->>'event_id')::uuid
  WHERE c.session_id=(e.body->>'session_id')::uuid
   AND c.asset_version_id=(e.body->'render_case'->'binding'->>'asset_version_id')::uuid
   AND c.source_lineage_sha=e.body->'render_case'->>'source_lineage_sha'
   AND c.approval_id=(e.body->'render_case'->>'approval_id')::uuid
   AND c.base_artifact_id=(e.body->'render_case'->'base_recipe'->>'artifact_id')::uuid
   AND c.base_sha=e.body->'render_case'->'base_recipe'->>'sha256'
   AND c.approved_artifact_id=(e.body->'render_case'->'approved_recipe'->>'artifact_id')::uuid
   AND c.approved_sha=e.body->'render_case'->'approved_recipe'->>'sha256'
   AND c.consent_live AND c.source_ready AND c.base_ready AND c.approved_ready
   AND NOT c.changed_paths ?| ARRAY['/crop','/selection']
   AND EXISTS(SELECT 1 FROM jsonb_array_elements(p_manifest->'consents') grant_body
    WHERE grant_body->>'consent_id'=c.consent_id::text
     AND grant_body->>'epoch'=c.consent_epoch::text)
 ) SELECT count(DISTINCT event_id),count(DISTINCT consent_id)
   INTO matched_count,used_count FROM matched;
 IF matched_count<>example_count OR used_count<>consent_count THEN RETURN false; END IF;
 FOR grant_item IN SELECT value FROM jsonb_array_elements(p_manifest->'consents') LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=w.project_id
    AND g.id=(grant_item->>'consent_id')::uuid
    AND g.epoch::text=grant_item->>'epoch' AND g.status='granted'
    AND g.purpose=CASE WHEN profile_row.person_id IS NULL THEN
     'learning_photographer' ELSE 'learning_person' END
    AND g.person_id IS NOT DISTINCT FROM profile_row.person_id) THEN
   RAISE EXCEPTION 'V23_TRAIN_CONSENT_CHANGED' USING ERRCODE='40001';
  END IF;
  PERFORM id FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
   AND g.id=(grant_item->>'consent_id')::uuid FOR SHARE;
  INSERT INTO photo.workflow_consent_links(tenant_id,project_id,workflow_id,
   consent_id,consent_epoch)
  VALUES(p_tenant,w.project_id,w.id,(grant_item->>'consent_id')::uuid,
   (grant_item->>'epoch')::bigint)
  ON CONFLICT(tenant_id,workflow_id,consent_id) DO NOTHING;
  IF NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   WHERE link.tenant_id=p_tenant AND link.workflow_id=w.id
    AND link.consent_id=(grant_item->>'consent_id')::uuid
    AND link.consent_epoch=(grant_item->>'epoch')::bigint) THEN
   RAISE EXCEPTION 'V23_TRAIN_CONSENT_LINK_CONFLICT' USING ERRCODE='23514';
  END IF;
 END LOOP;
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,
  artifact_id,sha256,payload)
 VALUES(p_tenant,s.id,0,manifest_doc.artifact_id,p_manifest_sha,
  jsonb_build_object('contract','TrainingManifest','owner_id',p_profile));
 UPDATE photo.snapshots SET artifact_id=root_doc.artifact_id,expires_at=NULL
  WHERE tenant_id=p_tenant AND id=s.id AND expires_at IS NOT NULL;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_TRAIN_FREEZE_RACE' USING ERRCODE='40001'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_v23_training_snapshot(uuid,uuid,uuid,uuid,uuid,bigint,jsonb,text,text)
 TO photo_scheduler;

-- A generic V2 graph seal must not activate a learning task while its
-- transport snapshot is still under construction.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v23_training_seal() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='v2' AND OLD.status='draft' AND NEW.status='queued' AND
  OLD.config_snapshot ? 'v23_training' AND
  NOT EXISTS(SELECT 1 FROM photo.v2_workflows w
   JOIN photo.snapshots s ON s.tenant_id=w.tenant_id
    AND s.id=w.input_snapshot_id
   JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id
    AND i.snapshot_id=s.id AND i.ordinal=0
   JOIN photo.backend_documents manifest ON manifest.tenant_id=OLD.tenant_id
    AND manifest.run_id=OLD.id AND manifest.owner_id=
     (OLD.config_snapshot->'v23_training'->>'profile_id')::uuid
    AND manifest.purpose='v23_training_manifest' AND manifest.state='ready'
   JOIN photo.backend_documents root ON root.tenant_id=OLD.tenant_id
    AND root.run_id=OLD.id AND root.owner_id=OLD.id
    AND root.purpose='v23_training_snapshot' AND root.state='ready'
   WHERE w.tenant_id=OLD.tenant_id AND w.run_id=OLD.id
    AND w.workflow_type='learning' AND w.renderer_version='v2.2'
    AND s.id=OLD.input_snapshot_id AND s.kind='v2' AND s.expires_at IS NULL
    AND s.digest=OLD.config_snapshot->'v23_training'->>'snapshot_digest'
    AND s.item_count=1 AND s.artifact_id=root.artifact_id
    AND root.sha256=OLD.config_snapshot->'v23_training'->>'snapshot_body_sha'
    AND i.artifact_id=manifest.artifact_id
    AND i.sha256=OLD.config_snapshot->'v23_training'->>'manifest_sha'
    AND i.payload=jsonb_build_object('contract','TrainingManifest',
     'owner_id',(OLD.config_snapshot->'v23_training'->>'profile_id')::uuid)) THEN
  RAISE EXCEPTION 'V23_TRAIN_SNAPSHOT_NOT_FROZEN' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v23_training_seal() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v23_training_seal() FROM PUBLIC;
CREATE TRIGGER v23_training_seal_guard BEFORE UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v23_training_seal();
