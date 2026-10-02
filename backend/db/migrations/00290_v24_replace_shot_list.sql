-- +goose Up
CREATE TRIGGER shot_lists_revision BEFORE UPDATE ON photo.shot_lists
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
-- +goose StatementBegin
CREATE FUNCTION photo.replace_v24_shot_list(p_tenant uuid,p_session uuid,p_expected bigint,
 p_document uuid,p_artifact uuid,p_sha text,p_body text,p_actor uuid,p_auth_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE session_row photo.shoot_sessions%ROWTYPE; list_row photo.shot_lists%ROWTYPE;
 stream uuid; next_version bigint; body jsonb; requirement jsonb; person uuid;
 seen_requirements uuid[]:='{}'; seen_people uuid[]; all_people uuid[]:='{}'; requirement_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_session IS NULL OR p_document IS NULL OR p_artifact IS NULL
 OR p_expected IS NULL OR p_expected<1 OR p_body IS NULL OR octet_length(p_body)>1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'invalid shot-list replacement' USING ERRCODE='22023';
 END IF;
 body:=p_body::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR body-ARRAY['schema_version','requirements']<>'{}'::jsonb
 OR jsonb_typeof(body->'requirements') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'invalid shot-list body' USING ERRCODE='22023';
 END IF;
 IF jsonb_array_length(body->'requirements')>200 THEN RAISE EXCEPTION 'too many requirements' USING ERRCODE='22023'; END IF;
 FOR requirement IN SELECT value FROM jsonb_array_elements(body->'requirements') LOOP
  IF jsonb_typeof(requirement) IS DISTINCT FROM 'object'
  OR requirement-ARRAY['id','label','person_ids','min_confirmed_frames','min_quality']<>'{}'::jsonb
  OR jsonb_typeof(requirement->'id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(requirement->'label') IS DISTINCT FROM 'string'
  OR length(requirement->>'label') NOT BETWEEN 1 AND 200
  OR jsonb_typeof(requirement->'person_ids') IS DISTINCT FROM 'array'
  OR jsonb_typeof(requirement->'min_confirmed_frames') IS DISTINCT FROM 'number'
  OR (requirement->>'min_confirmed_frames')::numeric NOT BETWEEN 1 AND 100
  OR trunc((requirement->>'min_confirmed_frames')::numeric)<>(requirement->>'min_confirmed_frames')::numeric
  OR jsonb_typeof(requirement->'min_quality') IS DISTINCT FROM 'number'
  OR (requirement->>'min_quality')::numeric NOT BETWEEN 0 AND 1 THEN
   RAISE EXCEPTION 'invalid shot requirement' USING ERRCODE='22023';
  END IF;
  requirement_id:=(requirement->>'id')::uuid;
  IF requirement_id='00000000-0000-0000-0000-000000000000'::uuid OR requirement_id=ANY(seen_requirements)
   OR jsonb_array_length(requirement->'person_ids')>100 THEN
   RAISE EXCEPTION 'invalid requirement identity' USING ERRCODE='22023';
  END IF;
  seen_requirements:=array_append(seen_requirements,requirement_id); seen_people:='{}';
  FOR person IN SELECT value::uuid FROM jsonb_array_elements_text(requirement->'person_ids') LOOP
   IF person IS NULL OR person='00000000-0000-0000-0000-000000000000'::uuid OR person=ANY(seen_people) THEN
    RAISE EXCEPTION 'invalid requirement person' USING ERRCODE='22023';
   END IF;
   seen_people:=array_append(seen_people,person);
   IF NOT person=ANY(all_people) THEN all_people:=array_append(all_people,person); END IF;
  END LOOP;
 END LOOP;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO session_row FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND id=p_session FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'session not found' USING ERRCODE='P0002'; END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,session_row.project_id,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'shot-list forbidden' USING ERRCODE='42501';
 END IF;
 IF session_row.revision<>p_expected OR session_row.state<>'open' THEN
  RAISE EXCEPTION 'shot-list revision conflict' USING ERRCODE='40001';
 END IF;
 FOR person IN SELECT value FROM unnest(all_people) value ORDER BY value LOOP
  PERFORM id FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=session_row.project_id AND id=person AND state='active' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
  PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=session_row.project_id AND person_id=person
   AND purpose='identity_matching' AND status='granted' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'V24_IDENTITY_CONSENT_REVOKED' USING ERRCODE='23514'; END IF;
 END LOOP;
 PERFORM u.document_id FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE u.tenant_id=p_tenant AND u.project_id=session_row.project_id AND u.shoot_session_id=p_session
 AND u.asset_version_id IS NULL AND u.document_id=p_document AND u.artifact_id=p_artifact AND u.sha256=p_sha
 AND u.bytes=octet_length(p_body) AND u.state='ready' AND a.state='ready' AND a.sha256=p_sha
 AND a.object_version=u.object_version AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR UPDATE OF u;
 IF NOT FOUND THEN RAISE EXCEPTION 'shot-list publication not ready' USING ERRCODE='23514'; END IF;
 SELECT * INTO list_row FROM photo.shot_lists WHERE tenant_id=p_tenant AND session_id=p_session FOR UPDATE;
 IF FOUND THEN
  SELECT d.stream_id INTO stream FROM photo.v2_documents d WHERE d.tenant_id=p_tenant AND d.id=list_row.document_id;
  PERFORM id FROM photo.v2_document_streams WHERE tenant_id=p_tenant AND id=stream AND stream_type='shot_list' FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'invalid shot-list stream' USING ERRCODE='23514'; END IF;
  SELECT max(version_no)+1 INTO next_version FROM photo.v2_documents WHERE tenant_id=p_tenant AND stream_id=stream;
 ELSE
  stream:=gen_random_uuid(); next_version:=1;
  INSERT INTO photo.v2_document_streams(id,tenant_id,project_id,stream_type) VALUES(stream,p_tenant,session_row.project_id,'shot_list');
 END IF;
 INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
 VALUES(p_document,p_tenant,session_row.project_id,stream,next_version,'ShotList',p_artifact,p_sha);
 UPDATE photo.v2_document_streams SET current_document_id=p_document WHERE tenant_id=p_tenant AND id=stream;
 IF list_row.id IS NULL THEN
  INSERT INTO photo.shot_lists(tenant_id,project_id,session_id,document_id)
  VALUES(p_tenant,session_row.project_id,p_session,p_document);
 ELSE
  UPDATE photo.shot_lists SET document_id=p_document WHERE tenant_id=p_tenant AND id=list_row.id;
 END IF;
 UPDATE photo.shoot_sessions SET updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_session RETURNING revision INTO p_expected;
 RETURN jsonb_build_object('session_id',p_session,'revision',p_expected::text,
  'document',jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha),'requirements',body->'requirements');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.replace_v24_shot_list(uuid,uuid,bigint,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.replace_v24_shot_list(uuid,uuid,bigint,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.replace_v24_shot_list(uuid,uuid,bigint,uuid,uuid,text,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
