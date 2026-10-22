-- +goose Up
-- Recheck the installed source/qualification/producer bindings after decision.
-- Selected-media service repeats this authority before and after physical IO.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_review_authority(p_tenant uuid,p_review uuid,
 p_actor uuid,p_session uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v photo.reviews%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 z photo.task_results%ROWTYPE;w photo.v2_workflows%ROWTYPE;body jsonb;
 pin jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_tenant IS NULL OR p_review IS NULL OR p_actor IS NULL OR p_session IS NULL
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO v FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND kind='image.relight' AND state IN('pending','decided') FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=v.run_id
  AND status IN('waiting_review','completed') AND kind='v2' AND graph_sealed AND NOT cancel_requested
  FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=v.task_id
  AND run_id=r.id AND operation='image.relight' AND operation_version='1'
  AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant
  AND id=t.canonical_result_id AND task_id=t.id AND attempt=t.attempt
  AND fencing_token=t.fencing_token AND review_required FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id
  AND workflow_type='preview' AND renderer_version='v2.2'
  AND input_snapshot_id=r.input_snapshot_id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=v.relight_params_jcs::jsonb;
 pin:=t.execution_profile->'relight_qualification';
 IF body IS NULL OR pin IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.v2_task_inputs i
  ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
  WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id AND i.task_id=t.id
   AND i.contract='Params' AND a.sha256=encode(sha256(convert_to(v.relight_params_jcs,'UTF8')),'hex')
   AND a.bytes=octet_length(v.relight_params_jcs) AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR body->'base'->'source'->>'artifact_id' IS DISTINCT FROM v.base_artifact_id::text
 OR body->'base'->'source'->>'sha256' IS DISTINCT FROM
  (SELECT sha256 FROM photo.artifacts WHERE tenant_id=p_tenant AND id=v.base_artifact_id)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.contract='WorkingImage'
   AND i.artifact_id=v.base_artifact_id AND i.sha256=body->'base'->'source'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=v.candidate_artifact_id AND a.sha256=v.candidate_hash
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
  WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
   AND l.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
   AND l.artifact_id=v.candidate_artifact_id AND l.sha256=v.candidate_hash)
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 THEN RETURN false;END IF;
 -- Keep all current authority, source and declared producer rows stable
 -- through the final decision UPDATE. Check expiry again after lock waits.
 PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=w.project_id AND user_id=p_actor FOR SHARE;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=w.input_snapshot_id FOR SHARE;
 PERFORM version.id FROM photo.asset_versions version JOIN photo.assets asset
  ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  WHERE version.tenant_id=p_tenant AND version.id=(body->'base'->>'asset_version_id')::uuid
  FOR SHARE OF version,asset;
 PERFORM producer_run.id FROM photo.runs producer_run WHERE producer_run.tenant_id=p_tenant
  AND producer_run.id IN(SELECT producer.run_id FROM photo.tasks producer JOIN photo.artifacts a
   ON a.tenant_id=producer.tenant_id AND a.created_by_task_id=producer.id
   JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id
   WHERE i.tenant_id=p_tenant AND i.task_id=t.id) ORDER BY producer_run.id FOR SHARE;
 PERFORM producer.id FROM photo.tasks producer WHERE producer.tenant_id=p_tenant
  AND producer.id IN(SELECT a.created_by_task_id FROM photo.artifacts a JOIN photo.v2_task_inputs i
   ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id WHERE i.tenant_id=p_tenant AND i.task_id=t.id)
  ORDER BY producer.id FOR SHARE;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i
  ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id ORDER BY a.id FOR SHARE OF a;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND
   (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL
    OR a.object_version IS NULL OR a.object_version IN('','null')
    OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())))
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
  JOIN photo.tasks producer ON producer.tenant_id=a.tenant_id AND producer.id=a.created_by_task_id
  JOIN photo.runs producer_run ON producer_run.tenant_id=producer.tenant_id AND producer_run.id=producer.run_id
  LEFT JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
   AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND
   (producer.state<>'succeeded' OR producer.cancel_requested OR producer_run.cancel_requested
    OR result.id IS NULL OR result.attempt IS DISTINCT FROM producer.attempt
    OR result.fencing_token IS DISTINCT FROM producer.fencing_token
    OR a.produced_attempt IS DISTINCT FROM producer.attempt
    OR a.produced_fence IS DISTINCT FROM producer.fencing_token))
 OR NOT EXISTS(SELECT 1 FROM photo.sessions session WHERE session.id=p_session
  AND session.user_id=p_actor AND session.revoked_at IS NULL AND session.expires_at>clock_timestamp())
 OR body->'calibration' IS DISTINCT FROM pin->'calibration'
 OR t.execution_profile->>'relight_runtime_sha' IS DISTINCT FROM pin->>'runtime_lock_sha'
 OR photo.v28_local_portrait_source_scope(p_tenant,w.project_id,w.input_snapshot_id,
  p_actor,p_session,body->'base') IS DISTINCT FROM true
 OR photo.lock_v28_relight_qualification(p_tenant,w.project_id,p_actor,p_session,pin) IS DISTINCT FROM true
 OR photo.v28_relight_review_preview_ready(p_tenant,p_review) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 -- Qualification/preview checks may wait for locks after the first expiry check.
 IF NOT EXISTS(SELECT 1 FROM photo.sessions session WHERE session.id=p_session
  AND session.user_id=p_actor AND session.revoked_at IS NULL AND session.expires_at>clock_timestamp())
 THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN false;
END $$;

ALTER FUNCTION photo.v28_relight_review_authority(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_review_authority(uuid,uuid,uuid,uuid) FROM PUBLIC;
ALTER FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid)
 RENAME TO get_v28_relight_review_selection_pre_authority;
REVOKE ALL ON FUNCTION photo.get_v28_relight_review_selection_pre_authority(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api;
CREATE FUNCTION photo.get_v28_relight_review_selection(
 p_tenant uuid,p_actor uuid,p_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v28_relight_review_authority(p_tenant,p_review,p_actor,p_session) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.get_v28_relight_review_selection_pre_authority(
  p_tenant,p_actor,p_session,p_review);
END $$;
ALTER FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
