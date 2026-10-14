-- Evaluate repeats the exact Apply source proof without requiring a browser
-- session that may have expired after training admission. The private copy is
-- reachable only through the fenced expansion and event-owned document gate.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; old_name text;
 old_session text; old_editor text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.v23_reviewed_dust_event_source(uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 old_name:='FUNCTION photo.v23_reviewed_dust_event_source(';
 old_session:='p_auth_session IS NULL OR ';
 old_editor:='IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1 OR
  (length(definition)-length(replace(definition,old_session,'')))/length(old_session)<>1 OR
  (length(definition)-length(replace(definition,old_editor,'')))/length(old_editor)<>1 THEN
  RAISE EXCEPTION 'Unexpected V23 dust source definition';
 END IF;
 definition:=replace(definition,old_name,
  'FUNCTION photo.v23_reviewed_dust_event_source_evaluate_core(');
 definition:=replace(definition,old_session,'');
 definition:=replace(definition,old_editor,'IF NOT FOUND THEN RETURN; END IF;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_dust_event_source_evaluate_core(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_dust_event_source_evaluate_core(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_reviewed_dust_authority(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,
 p_event uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(review_id uuid,review_run_id uuid,actor_id uuid,
 decision_hash text,asset_version_id uuid,image_id uuid,image_sha text,
 stage_image_id uuid,stage_image_sha text,proposal_id uuid,proposal_sha text,
 params_id uuid,params_sha text,bucket text,object_key text,
 object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE root record; source_row record; run_row photo.runs%ROWTYPE;
 document photo.backend_documents%ROWTYPE; project_id uuid;
 h bytea; expected_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_event IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-dust/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact<>expected_id THEN RETURN; END IF;
 SELECT * INTO root FROM photo.v23_evaluate_frozen_root(
  p_tenant,p_run,p_expansion,p_token,p_artifact,p_sha,
  'FrozenDustHoldoutEvidence');
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO run_row FROM photo.runs r WHERE r.tenant_id=p_tenant AND
  r.id=p_run AND r.kind='v2' AND r.status='running' AND
  NOT r.cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p
  WHERE p.tenant_id=p_tenant AND p.id=NULLIF(
   run_row.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 IF project_id IS NULL THEN RETURN; END IF;
 SELECT * INTO document FROM photo.backend_documents d WHERE
  d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_event AND
  d.purpose='v23_dust_holdout' AND d.artifact_id=p_artifact AND
  d.sha256=p_sha AND d.state='ready' AND
  d.object_version=root.object_version AND d.bucket=root.bucket AND
  d.bytes=root.bytes FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO source_row FROM photo.v23_reviewed_dust_event_source_evaluate_core(
  p_tenant,project_id,run_row.created_by,NULL::uuid,p_event);
 IF NOT FOUND OR source_row.actor_id IS DISTINCT FROM run_row.created_by OR
  source_row.asset_version_id IS NULL OR
  source_row.image_id IS NULL OR source_row.stage_image_id IS NULL OR
  source_row.proposal_id IS NULL OR source_row.params_id IS NULL OR
  source_row.decision_hash !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 RETURN QUERY SELECT source_row.review_id,source_row.review_run_id,
  source_row.actor_id,source_row.decision_hash,
  source_row.asset_version_id,source_row.image_id,source_row.image_sha,
  source_row.stage_image_id,source_row.stage_image_sha,
  source_row.proposal_id,source_row.proposal_sha,
  source_row.params_id,source_row.params_sha,
  root.bucket,root.object_key,root.object_version,root.bytes;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_reviewed_dust_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_reviewed_dust_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_reviewed_dust_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
