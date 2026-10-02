-- V23 RecordCorrection: one trusted admission boundary for immutable edits.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.record_v2_correction(
 p_tenant uuid,p_actor uuid,p_auth_session uuid,p_request jsonb,p_paths jsonb,
 p_base_sha text,p_approved_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE
 p_client uuid; p_shoot uuid; p_version uuid; p_base uuid; p_approved uuid;
 p_person uuid; p_group uuid; p_consent uuid; p_epoch bigint;
 p_reason text; p_learn boolean; p_project uuid; p_asset uuid;
 stream_row photo.v2_document_streams%ROWTYPE;
 base_row photo.v2_documents%ROWTYPE; approved_row photo.v2_documents%ROWTYPE;
 approval_row photo.v2_document_approvals%ROWTYPE;
 existing photo.correction_events%ROWTYPE; inserted photo.correction_events%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_request IS NULL OR jsonb_typeof(p_request)<>'object' OR
  p_paths IS NULL OR jsonb_typeof(p_paths)<>'array' OR
  jsonb_array_length(p_paths)<1 OR jsonb_array_length(p_paths)>6 OR
  coalesce(p_base_sha,'') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_approved_sha,'') !~ '^[0-9a-f]{64}$' OR
  (SELECT count(*) FROM jsonb_object_keys(p_request))<>11 OR
  p_request ?& ARRAY['client_event_id','session_id','asset_version_id',
   'base_document_id','approved_document_id','reason','learn','person_id',
   'scene_group_id','consent_id','consent_epoch'] IS NOT TRUE OR
  jsonb_typeof(p_request->'learn') IS DISTINCT FROM 'boolean' OR
  jsonb_typeof(p_request->'reason') IS DISTINCT FROM 'string' OR
  jsonb_typeof(p_request->'consent_epoch') NOT IN ('string','null') OR
  EXISTS(SELECT 1 FROM jsonb_array_elements(p_paths) x
   WHERE jsonb_typeof(x)<>'string' OR x #>> '{}' NOT IN
   ('/ev','/log_r','/log_g','/log_b','/saturation_delta','/retouch_delta')) OR
  (SELECT count(DISTINCT x) FROM jsonb_array_elements(p_paths) x)<>
   jsonb_array_length(p_paths) THEN
  RAISE EXCEPTION 'V23_CORRECTION_INVALID' USING ERRCODE='22023';
 END IF;
 BEGIN
  p_client:=(p_request->>'client_event_id')::uuid;
  p_shoot:=(p_request->>'session_id')::uuid;
  p_version:=(p_request->>'asset_version_id')::uuid;
  p_base:=(p_request->>'base_document_id')::uuid;
  p_approved:=(p_request->>'approved_document_id')::uuid;
  p_person:=(p_request->>'person_id')::uuid;
  p_group:=(p_request->>'scene_group_id')::uuid;
  p_consent:=(p_request->>'consent_id')::uuid;
  IF p_request->>'consent_epoch' IS NOT NULL THEN
   IF p_request->>'consent_epoch' !~ '^[1-9][0-9]{0,18}$' THEN
    RAISE EXCEPTION 'V23_CORRECTION_EPOCH' USING ERRCODE='22023';
   END IF;
   p_epoch:=(p_request->>'consent_epoch')::bigint;
  END IF;
  p_learn:=(p_request->>'learn')::boolean;
 EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
  RAISE EXCEPTION 'V23_CORRECTION_INVALID' USING ERRCODE='22023';
 END;
 p_reason:=p_request->>'reason';
 IF p_client IS NULL OR p_shoot IS NULL OR p_version IS NULL OR
  p_base IS NULL OR p_approved IS NULL OR p_base=p_approved OR
  p_client='00000000-0000-0000-0000-000000000000'::uuid OR
  p_learn IS NULL OR p_reason IS NULL OR p_reason NOT IN
  ('exposure','color','crop','retouch','selection','other') OR
  (p_learn AND (p_consent IS NULL OR p_epoch IS NULL)) OR
  (NOT p_learn AND (p_consent IS NOT NULL OR p_epoch IS NOT NULL)) THEN
  RAISE EXCEPTION 'V23_CORRECTION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_TENANT_UNAVAILABLE' USING ERRCODE='42501'; END IF;
 SELECT a.project_id,a.id INTO p_project,p_asset FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.review_state='accepted'
  AND v.created_by_run_id IS NULL AND a.deleted_at IS NULL
  AND a.upload_state='verified' FOR SHARE OF a,v;
 IF p_project IS NULL OR NOT photo.v2_editor_authorized(
  p_tenant,p_project,p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V23_CORRECTION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 -- The tenant row lock serializes concurrent admissions for this tenant;
 -- correction_events itself is immutable and grants no UPDATE privilege.
 SELECT * INTO existing FROM photo.correction_events
  WHERE tenant_id=p_tenant AND client_event_id=p_client;
 IF FOUND THEN
  IF existing.project_id<>p_project OR existing.session_id<>p_shoot OR
   existing.asset_version_id<>p_version OR existing.base_document_id<>p_base OR
   existing.approved_document_id<>p_approved OR existing.reason<>p_reason OR
   existing.learn<>p_learn OR existing.person_id IS DISTINCT FROM p_person OR
   existing.scene_group_id IS DISTINCT FROM p_group OR
   existing.consent_id IS DISTINCT FROM p_consent OR
   existing.consent_epoch IS DISTINCT FROM p_epoch OR
   existing.changed_paths IS DISTINCT FROM p_paths THEN
   RAISE EXCEPTION 'V23_CORRECTION_REPLAY_CONFLICT' USING ERRCODE='40001';
  END IF;
  RETURN (to_jsonb(existing)-'tenant_id') ||
   jsonb_build_object('consent_epoch',existing.consent_epoch::text);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.shoot_sessions s
  JOIN photo.collection_items ci ON ci.tenant_id=s.tenant_id
   AND ci.collection_id=s.collection_id AND ci.asset_version_id=p_version
  WHERE s.tenant_id=p_tenant AND s.project_id=p_project AND s.id=p_shoot) THEN
  RAISE EXCEPTION 'V23_CORRECTION_SESSION_SOURCE' USING ERRCODE='23514';
 END IF;
 SELECT * INTO stream_row FROM photo.v2_document_streams
  WHERE tenant_id=p_tenant AND project_id=p_project AND
   asset_version_id=p_version AND stream_type='draft' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_CORRECTION_STREAM' USING ERRCODE='23514'; END IF;
 SELECT * INTO base_row FROM photo.v2_documents WHERE tenant_id=p_tenant
  AND project_id=p_project AND stream_id=stream_row.id AND id=p_base
  AND document_type='EditingRecipeV3';
 SELECT * INTO approved_row FROM photo.v2_documents WHERE tenant_id=p_tenant
  AND project_id=p_project AND stream_id=stream_row.id AND id=p_approved
  AND document_type='EditingRecipeV3';
 IF base_row.id IS NULL OR approved_row.id IS NULL OR
  base_row.version_no>=approved_row.version_no OR
  base_row.sha256<>p_base_sha OR approved_row.sha256<>p_approved_sha OR
  (base_row.version_no<>1 AND NOT EXISTS(SELECT 1
   FROM photo.v2_document_approvals x WHERE x.tenant_id=p_tenant
   AND x.project_id=p_project AND x.document_id=p_base)) THEN
  RAISE EXCEPTION 'V23_CORRECTION_DOCUMENTS' USING ERRCODE='23514';
 END IF;
 SELECT * INTO approval_row FROM photo.v2_document_approvals
  WHERE tenant_id=p_tenant AND project_id=p_project AND
   document_id=p_approved AND recipe_sha=p_approved_sha;
 IF approval_row.id IS NULL OR NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=base_row.artifact_id AND a.sha256=p_base_sha AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) OR NOT EXISTS(
  SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=approved_row.artifact_id AND a.sha256=p_approved_sha AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'V23_CORRECTION_UNAPPROVED' USING ERRCODE='23514';
 END IF;
 IF p_group IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM photo.scene_members m JOIN photo.scene_groups g
   ON g.tenant_id=m.tenant_id AND g.id=m.group_id
  JOIN photo.session_analyses an ON an.tenant_id=g.tenant_id AND an.id=g.analysis_id
  WHERE m.tenant_id=p_tenant AND m.project_id=p_project AND
   m.asset_version_id=p_version AND g.id=p_group AND an.session_id=p_shoot) THEN
  RAISE EXCEPTION 'V23_CORRECTION_SCENE' USING ERRCODE='23514';
 END IF;
 IF p_person IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.project_persons p
  WHERE p.tenant_id=p_tenant AND p.project_id=p_project AND
   p.id=p_person AND p.state='active') THEN
  RAISE EXCEPTION 'V23_CORRECTION_PERSON' USING ERRCODE='23514';
 END IF;
 IF p_learn AND NOT EXISTS(SELECT 1 FROM photo.consent_grants c
  WHERE c.tenant_id=p_tenant AND c.project_id=p_project AND c.id=p_consent
   AND c.status='granted' AND c.epoch=p_epoch AND
   c.person_id IS NOT DISTINCT FROM p_person AND
   c.purpose=CASE WHEN p_person IS NULL THEN 'learning_photographer'
    ELSE 'learning_person' END) THEN
  RAISE EXCEPTION 'V23_CORRECTION_CONSENT' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.correction_events(tenant_id,project_id,session_id,
  asset_version_id,base_document_id,approved_document_id,changed_paths,
  reason,learn,consent_id,consent_epoch,approved_by,client_event_id,
  person_id,scene_group_id)
 VALUES(p_tenant,p_project,p_shoot,p_version,p_base,p_approved,p_paths,
  p_reason,p_learn,p_consent,p_epoch,approval_row.actor_id,p_client,p_person,p_group)
 RETURNING * INTO inserted;
 RETURN (to_jsonb(inserted)-'tenant_id') ||
  jsonb_build_object('consent_epoch',inserted.consent_epoch::text);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_v2_correction(uuid,uuid,uuid,jsonb,jsonb,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v2_correction(uuid,uuid,uuid,jsonb,jsonb,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v2_correction(uuid,uuid,uuid,jsonb,jsonb,text,text) TO photo_api;
