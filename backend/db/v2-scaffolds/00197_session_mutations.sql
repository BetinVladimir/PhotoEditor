-- Server validates SessionPolicy/CapturePolicy JSON Schema and qualified
-- Calibration bytes before calling these DB-only commands. SQL repeats
-- authoritative principal, project, artifact and model-qualification checks.
-- +goose StatementBegin
CREATE FUNCTION photo.ensure_v22_photographer_grant(p_tenant uuid,p_project uuid,
 p_actor uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE grant_id uuid;
BEGIN
 INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,person_id,status,
  epoch,policy_version,actor_id)
 VALUES(p_tenant,p_project,'learning_photographer',NULL,'revoked',1,'v2.2',p_actor)
 ON CONFLICT DO NOTHING;
 SELECT id INTO grant_id FROM photo.consent_grants WHERE tenant_id=p_tenant
  AND project_id=p_project AND purpose='learning_photographer' AND person_id IS NULL;
 IF grant_id IS NULL THEN
  RAISE EXCEPTION 'V22_PROJECT_CONSENT_BOOTSTRAP_FAILED' USING ERRCODE='23514';
 END IF;
 RETURN grant_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.ensure_v22_photographer_grant(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ensure_v22_photographer_grant(uuid,uuid,uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.v22_session_policy_ready(p_tenant uuid,p_project uuid,
 p_policy jsonb,p_model_lock_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE calibration_id uuid; calibration_sha text; offset_item jsonb;
 offset_hashes text[]:=ARRAY[]::text[];
BEGIN
 IF jsonb_typeof(p_policy) IS DISTINCT FROM 'object' OR
  (p_policy-'schema_version'-'time_gap_seconds'-'neighbor_count'-'protect_manual_picks'-'camera_offsets'-'calibration')<>'{}'::jsonb OR
  NOT (p_policy ?& ARRAY['schema_version','time_gap_seconds','neighbor_count',
   'protect_manual_picks','camera_offsets','calibration']) OR
  p_policy->>'schema_version' IS DISTINCT FROM '1' OR
  p_policy->>'protect_manual_picks' IS DISTINCT FROM 'true' OR
  jsonb_typeof(p_policy->'schema_version') IS DISTINCT FROM 'number' OR
  jsonb_typeof(p_policy->'time_gap_seconds') IS DISTINCT FROM 'number' OR
  jsonb_typeof(p_policy->'neighbor_count') IS DISTINCT FROM 'number' OR
  jsonb_typeof(p_policy->'protect_manual_picks') IS DISTINCT FROM 'boolean' OR
  jsonb_typeof(p_policy->'calibration') IS DISTINCT FROM 'object' OR
  jsonb_typeof(p_policy->'camera_offsets') IS DISTINCT FROM 'array' OR
  ((p_policy->'calibration')-'artifact_id'-'sha256')<>'{}'::jsonb OR
  NOT ((p_policy->'calibration') ?& ARRAY['artifact_id','sha256']) OR
  coalesce(p_policy->'calibration'->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' OR
  coalesce(p_policy->'calibration'->>'sha256','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_model_lock_sha,'') !~ '^[0-9a-f]{64}$' OR
  octet_length(p_policy::text)>1048576 THEN RETURN false; END IF;
 IF coalesce(p_policy->>'time_gap_seconds','') !~ '^[0-9]{1,4}$' OR
  coalesce(p_policy->>'neighbor_count','') !~ '^[0-9]{1,2}$' THEN RETURN false; END IF;
 IF (p_policy->>'time_gap_seconds')::int NOT BETWEEN 30 AND 7200 OR
  (p_policy->>'neighbor_count')::int NOT BETWEEN 1 AND 64 OR
  jsonb_array_length(p_policy->'camera_offsets')>32 THEN RETURN false; END IF;
 FOR offset_item IN SELECT value FROM jsonb_array_elements(p_policy->'camera_offsets') LOOP
  IF jsonb_typeof(offset_item) IS DISTINCT FROM 'object' OR
   (offset_item-'camera_hash'-'seconds')<>'{}'::jsonb OR
   NOT (offset_item ?& ARRAY['camera_hash','seconds']) OR
   jsonb_typeof(offset_item->'seconds') IS DISTINCT FROM 'number' OR
   coalesce(offset_item->>'camera_hash','') !~ '^[0-9a-f]{64}$' OR
   coalesce(offset_item->>'seconds','') !~ '^(-|)[0-9]{1,5}$' THEN RETURN false; END IF;
  IF (offset_item->>'seconds')::int NOT BETWEEN -86400 AND 86400 THEN RETURN false; END IF;
  IF offset_item->>'camera_hash'=ANY(offset_hashes) THEN RETURN false; END IF;
  offset_hashes:=array_append(offset_hashes,offset_item->>'camera_hash');
 END LOOP;
 calibration_id:=(p_policy->'calibration'->>'artifact_id')::uuid;
 calibration_sha:=p_policy->'calibration'->>'sha256';
 RETURN EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=calibration_id AND a.sha256=calibration_sha AND a.state='ready'
  AND a.media_type='application/json' AND a.verified_at IS NOT NULL)
 AND EXISTS(SELECT 1 FROM photo.model_qualifications q WHERE q.tenant_id=p_tenant
  AND q.project_id=p_project AND q.operation='session.describe'
  AND q.operation_version='1' AND q.model_lock_sha=p_model_lock_sha AND q.status='passed');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_session_policy_ready(uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_session_policy_ready(uuid,uuid,jsonb,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.create_v22_session(p_tenant uuid,p_project uuid,p_collection uuid,
 p_name text,p_policy jsonb,p_capture_policy jsonb,p_model_lock_sha text,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE created photo.shoot_sessions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_collection IS NULL OR p_actor IS NULL
  OR p_session IS NULL OR length(coalesce(p_name,'')) NOT BETWEEN 1 AND 200 OR
  (p_capture_policy IS NOT NULL AND (jsonb_typeof(p_capture_policy)<>'object'
   OR octet_length(p_capture_policy::text)>1048576)) THEN
  RAISE EXCEPTION 'V22_SESSION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V22_SESSION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.collections c WHERE c.tenant_id=p_tenant
  AND c.project_id=p_project AND c.id=p_collection) OR
  NOT photo.v22_session_policy_ready(p_tenant,p_project,p_policy,p_model_lock_sha) THEN
  RAISE EXCEPTION 'V22_SESSION_POLICY_UNAVAILABLE' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,
  policy,capture_policy,consent_epoch)
 VALUES(p_tenant,p_project,p_collection,p_name,'open',p_policy,p_capture_policy,1)
 RETURNING * INTO created;
 PERFORM photo.ensure_v22_photographer_grant(p_tenant,p_project,p_actor);
 RETURN jsonb_build_object('id',created.id,'revision',created.revision::text,
  'state',created.state);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_v22_session(uuid,uuid,uuid,text,jsonb,jsonb,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v22_session(uuid,uuid,uuid,text,jsonb,jsonb,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v22_session(uuid,uuid,uuid,text,jsonb,jsonb,text,uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.update_v22_session(p_tenant uuid,p_project uuid,p_id uuid,
 p_expected bigint,p_name text,p_state text,p_policy jsonb,p_capture_policy jsonb,
 p_model_lock_sha text,p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_row photo.shoot_sessions%ROWTYPE; changed photo.shoot_sessions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_id IS NULL OR p_expected IS NULL
  OR p_expected<1 OR length(coalesce(p_name,'')) NOT BETWEEN 1 AND 200 OR
  coalesce(p_state,'') NOT IN('open','closed','archived') OR
  (p_capture_policy IS NOT NULL AND (jsonb_typeof(p_capture_policy)<>'object'
   OR octet_length(p_capture_policy::text)>1048576)) THEN
  RAISE EXCEPTION 'V22_SESSION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V22_SESSION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO current_row FROM photo.shoot_sessions WHERE tenant_id=p_tenant
  AND project_id=p_project AND id=p_id FOR UPDATE;
 IF current_row.id IS NULL THEN
  RAISE EXCEPTION 'V22_SESSION_NOT_FOUND' USING ERRCODE='P0002';
 END IF;
 IF current_row.revision<>p_expected OR current_row.state='archived' OR
  (current_row.state='open' AND p_state='archived') THEN
  RAISE EXCEPTION 'V22_SESSION_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 IF NOT photo.v22_session_policy_ready(p_tenant,p_project,p_policy,p_model_lock_sha) THEN
  RAISE EXCEPTION 'V22_SESSION_POLICY_UNAVAILABLE' USING ERRCODE='23514';
 END IF;
 UPDATE photo.shoot_sessions SET name=p_name,state=p_state,policy=p_policy,
  capture_policy=p_capture_policy WHERE tenant_id=p_tenant AND id=p_id
 RETURNING * INTO changed;
 RETURN jsonb_build_object('id',changed.id,'revision',changed.revision::text,
  'state',changed.state);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.update_v22_session(uuid,uuid,uuid,bigint,text,text,jsonb,jsonb,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.update_v22_session(uuid,uuid,uuid,bigint,text,text,jsonb,jsonb,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.update_v22_session(uuid,uuid,uuid,bigint,text,text,jsonb,jsonb,text,uuid,uuid) TO photo_api;
