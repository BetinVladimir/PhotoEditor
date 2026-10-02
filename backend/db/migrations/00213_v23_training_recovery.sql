-- Persist exact JCS compiler inputs in the V2.Admit transaction. The regular
-- V2 graph seal replaces this pending response with the final 202 ticket.
-- +goose Up
GRANT EXECUTE ON FUNCTION photo.begin_v23_training_draft(uuid,uuid,uuid,uuid,bigint,uuid,uuid,text,text,jsonb,bigint,text,text)
 TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.save_v23_training_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; r photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; q photo.quota_reservations%ROWTYPE;
 manifest_text text; snapshot_text text; policy_text text;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_run IS NULL OR p_key IS NULL OR p_request_sha IS NULL OR
  jsonb_typeof(p_body) IS DISTINCT FROM 'object' OR
  (p_body-'schema_version'-'manifest_jcs'-'manifest_sha'-
   'snapshot_jcs'-'snapshot_body_sha'-'snapshot_digest'-
   'policy_jcs'-'policy_sha'-'reserved_bytes'-'expected_revision'-
   'profile_id'-'project_id')<>'{}'::jsonb OR
  p_body->>'schema_version' IS DISTINCT FROM '1' OR
  coalesce(p_body->>'manifest_sha','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'snapshot_body_sha','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'snapshot_digest','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'policy_sha','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'reserved_bytes','') !~ '^[1-9][0-9]{0,15}$' OR
  coalesce(p_body->>'expected_revision','') !~ '^[1-9][0-9]{0,18}$' OR
  octet_length(p_body::text)>16777216 THEN
  RAISE EXCEPTION 'V23_TRAIN_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 manifest_text:=p_body->>'manifest_jcs';
 snapshot_text:=p_body->>'snapshot_jcs';
 policy_text:=p_body->>'policy_jcs';
 IF manifest_text IS NULL OR snapshot_text IS NULL OR policy_text IS NULL OR
  octet_length(manifest_text) NOT BETWEEN 2 AND 8388608 OR
  octet_length(snapshot_text) NOT BETWEEN 2 AND 1048576 OR
  octet_length(policy_text) NOT BETWEEN 2 AND 1048576 OR
  encode(public.digest(convert_to(manifest_text,'UTF8'),'sha256'),'hex')<>
   p_body->>'manifest_sha' OR
  encode(public.digest(convert_to(snapshot_text,'UTF8'),'sha256'),'hex')<>
   p_body->>'snapshot_body_sha' OR
  encode(public.digest(convert_to(policy_text,'UTF8'),'sha256'),'hex')<>
   p_body->>'policy_sha' OR
  jsonb_typeof(manifest_text::jsonb)<>'object' OR
  jsonb_typeof(snapshot_text::jsonb)<>'object' OR
  jsonb_typeof(policy_text::jsonb)<>'object' OR
  (manifest_text::jsonb)->>'profile_id' IS DISTINCT FROM p_body->>'profile_id' OR
  (snapshot_text::jsonb)->>'snapshot_digest' IS DISTINCT FROM
   p_body->>'snapshot_digest' OR
  (snapshot_text::jsonb)->>'project_id' IS DISTINCT FROM p_body->>'project_id'
  THEN RAISE EXCEPTION 'V23_TRAIN_RECOVERY_BYTES_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
  AND i.principal_type='user' AND i.principal_id=p_actor
  AND i.route='V2.Admit' AND i.key=p_key AND i.request_hash=p_request_sha
  FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=p_run AND x.id=idem.resource_id AND x.created_by=p_actor
  AND x.kind='v2' FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning';
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved';
 IF idem.id IS NULL OR idem.response_status IS NOT NULL OR r.id IS NULL OR
  r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR
  w.id IS NULL OR q.id IS NULL OR q.expires_at<=clock_timestamp() OR
  NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_auth_session) OR
  w.project_id::text IS DISTINCT FROM p_body->>'project_id' OR
  w.policy_sha IS DISTINCT FROM p_body->>'policy_sha' OR
  q.bytes::text IS DISTINCT FROM p_body->>'reserved_bytes' OR
  r.config_snapshot->'v23_training'->>'profile_id' IS DISTINCT FROM
   p_body->>'profile_id' OR
  r.config_snapshot->'v23_training'->>'expected_revision' IS DISTINCT FROM
   p_body->>'expected_revision' OR
  r.config_snapshot->'v23_training'->>'manifest_sha' IS DISTINCT FROM
   p_body->>'manifest_sha' OR
  r.config_snapshot->'v23_training'->>'snapshot_body_sha' IS DISTINCT FROM
   p_body->>'snapshot_body_sha' OR
  r.config_snapshot->'v23_training'->>'snapshot_digest' IS DISTINCT FROM
   p_body->>'snapshot_digest' THEN
  RAISE EXCEPTION 'V23_TRAIN_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 IF idem.response_body IS NOT NULL AND idem.response_body<>p_body THEN
  RAISE EXCEPTION 'V23_TRAIN_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 UPDATE photo.idempotency_records SET response_body=p_body WHERE id=idem.id
  AND response_body IS NULL;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v23_training_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_replay(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,run_id uuid,workflow_id uuid,recovery jsonb)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; r photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_run IS NULL OR p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR
  p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN
  RETURN QUERY SELECT 'invalid'::text,NULL::uuid,NULL::uuid,NULL::jsonb;
  RETURN;
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
  AND i.principal_type='user' AND i.principal_id=p_actor
  AND i.route='V2.Admit' AND i.key=p_key;
 IF idem.id IS NULL THEN
  RETURN QUERY SELECT 'missing'::text,NULL::uuid,NULL::uuid,NULL::jsonb;
  RETURN;
 END IF;
 IF idem.request_hash<>p_request_sha THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::uuid,NULL::uuid,NULL::jsonb;
  RETURN;
 END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=idem.resource_id AND x.id=p_run AND x.created_by=p_actor
  AND x.kind='v2';
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.workflow_type='learning';
 IF r.id IS NULL OR w.id IS NULL OR NOT photo.v2_editor_authorized(
  p_tenant,w.project_id,p_actor,p_auth_session) THEN
  RETURN QUERY SELECT 'expired'::text,NULL::uuid,NULL::uuid,NULL::jsonb;
  RETURN;
 END IF;
 IF idem.response_status=202 AND r.graph_sealed THEN
  RETURN QUERY SELECT 'replayed'::text,r.id,w.id,NULL::jsonb; RETURN;
 END IF;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved'
  AND x.expires_at>clock_timestamp();
 IF idem.response_status IS NULL AND idem.response_body IS NOT NULL AND
  r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested AND
  q.id IS NOT NULL AND q.bytes::text=idem.response_body->>'reserved_bytes' AND
  r.config_snapshot->'v23_training'->>'manifest_sha'=
   idem.response_body->>'manifest_sha' AND
  r.config_snapshot->'v23_training'->>'snapshot_body_sha'=
   idem.response_body->>'snapshot_body_sha' AND
  r.config_snapshot->'v23_training'->>'snapshot_digest'=
   idem.response_body->>'snapshot_digest' THEN
  RETURN QUERY SELECT 'pending'::text,r.id,w.id,idem.response_body;
  RETURN;
 END IF;
 RETURN QUERY SELECT 'expired'::text,NULL::uuid,NULL::uuid,NULL::jsonb;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_replay(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_replay(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_replay(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;
