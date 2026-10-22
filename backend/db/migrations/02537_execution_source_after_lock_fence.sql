-- +goose Up
-- Spent run quota is still part of the original immutable output allocation.
-- +goose StatementBegin
DO $$
DECLARE signature text;source text;definition text;needle text:='q.bytes=expected::bigint';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.semantic_shown_removal_task_live(uuid)',
 'photo.semantic_shown_background_cleanup_task_live(uuid)','photo.semantic_shown_retouch_task_live(uuid)'] LOOP
  SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
  WHERE oid=signature::regprocedure AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
  IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'SHOWN_OUTPUT_ACCOUNTING_SOURCE_SHAPE';END IF;
  EXECUTE replace(definition,source,replace(source,needle,
   'q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND allocated.run_id=r.id),0)=expected'));
 END LOOP;
END $$;
-- Retry claim retains the same run allocation even after an earlier attempt uploaded.
DO $$
DECLARE target record;source text;definition text;count_rows integer:=0;needle text:='q.bytes=expected::bigint';
BEGIN
 FOR target IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.proname LIKE 'claim_task%'
 AND strpos(p.prosrc,needle)>0 LOOP
  SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
  WHERE oid=target.oid AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner'
  AND oidvectortypes(proargtypes)='uuid, integer, uuid, jsonb';
  IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'SHOWN_CLAIM_ACCOUNTING_SOURCE_SHAPE';END IF;
  EXECUTE replace(definition,source,replace(source,needle,
   'q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND allocated.run_id=r.id),0)=expected'));
  count_rows:=count_rows+1;
 END LOOP;
 IF count_rows<>4 THEN RAISE EXCEPTION 'SHOWN_CLAIM_ACCOUNTING_SOURCE_COUNT';END IF;
END $$;
-- Own shown exposure copy has the same current source boundary as other copies.
CREATE FUNCTION photo.semantic_shown_exposure_task_live(p_task uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 IF t.operation IS DISTINCT FROM 'semantic.adopt_shown_exposure' THEN RETURN true;END IF;
 IF t.state IS DISTINCT FROM 'running' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 project:=(r.config_snapshot->>'project_id')::uuid;
 IF t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.id IS NULL OR r.kind IS DISTINCT FROM 'apply' OR r.status NOT IN('queued','running')
 OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 OR jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array'
 OR photo.v27_downstream_profile_ready(t.tenant_id,project,t.operation,t.operation_version,'cpu',t.execution_profile) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_review_batches b JOIN photo.semantic_targets target
 ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project AND b.actor_id=r.created_by
 AND b.action='apply' AND target.state='queued' AND target.project_id=project
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_exposure:'||target.id::text)
 AND (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text
 AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1)
 OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
 AND (i.project_id IS DISTINCT FROM project OR a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256
 OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
 OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())))
 THEN RETURN false;END IF;
 SELECT sum((execution_profile->'resource_estimate'->>'output_bytes')::numeric) INTO expected
 FROM photo.tasks WHERE tenant_id=t.tenant_id AND run_id=r.id;
 IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL AND q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND allocated.run_id=r.id),0)=expected
 AND q.expires_at>clock_timestamp()) THEN
 RETURN false;END IF;

 RETURN EXISTS(SELECT 1 FROM photo.v2_review_batches b
 JOIN photo.semantic_targets target ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=b.child_run_id
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.runs preview ON preview.tenant_id=intent.tenant_id
  AND preview.config_snapshot->'semantic_preapply_preview'->'params'->>'intent_id'=intent.id::text
  AND preview.config_snapshot->'semantic_preapply_preview'->'params'->>'target_id'=target.id::text
 JOIN photo.tasks producer ON producer.tenant_id=preview.tenant_id AND producer.run_id=preview.id
  AND producer.id::text=preview.v27_preapply_preview_receipt->>'task_id'
 WHERE b.tenant_id=t.tenant_id AND b.child_run_id=r.id AND b.project_id=project AND b.actor_id=r.created_by
 AND target.state='queued' AND preview.kind='preview' AND preview.graph_sealed AND NOT preview.cancel_requested
 AND preview.created_by=r.created_by AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND producer.canonical_result_id::text=preview.v27_preapply_preview_receipt->>'result_id'
 AND producer.attempt::text=preview.v27_preapply_preview_receipt->>'attempt'
 AND producer.fencing_token::text=preview.v27_preapply_preview_receipt->>'fence'
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=t.tenant_id AND input.task_id=t.id
  AND input.artifact_id::text=preview.v27_preapply_preview_receipt->'presentation'->>'artifact_id'
  AND input.sha256=preview.v27_preapply_preview_receipt->'presentation'->>'sha256')
 AND photo.v2_editor_authorized(t.tenant_id,project,r.created_by,(preview.config_snapshot->'semantic_preapply_preview'->>'session_id')::uuid) IS TRUE);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_task_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_task_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
CREATE FUNCTION photo.execution_source_live(p_task uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.operation_registry op ON op.operation=t.operation AND op.version=t.operation_version
 WHERE t.id=p_task AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend')
 AND (t.operation NOT IN ('layer.compose','session.describe','session.plan','learning.fit','learning.evaluate','capture.assess','identity.compare','semantic.transfer','portrait.open_eyes','portrait.glare','image.reflection','portrait.hair','portrait.clothing','image.relight','optical.lens','optical.perspective','optical.negative','optical.panorama','optical.focus_stack','provenance.sign','v2.delivery.assemble','v2.interchange.import','v2.interchange.export','v2.semantic.assemble','v2.interchange.parse','v2.delivery.prepare','region.preview','render.compose','semantic.local_exposure','optical.perspective_lines','semantic.adopt_shown_exposure','semantic.preview_remove_object','semantic.adopt_shown_removal','semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region','semantic.preview_retouch_region') OR op.v2_enabled))
 AND photo.semantic_shown_exposure_task_live(p_task) IS TRUE
 AND photo.semantic_shown_media_task_source_live(p_task) IS TRUE
 AND photo.semantic_removal_preview_task_live(p_task) IS TRUE
 AND photo.semantic_shown_removal_task_live(p_task) IS TRUE
 AND photo.semantic_background_cleanup_preview_task_live(p_task) IS TRUE
 AND photo.semantic_shown_background_cleanup_task_live(p_task) IS TRUE
 AND photo.semantic_retouch_preview_task_live(p_task) IS TRUE
 AND photo.semantic_shown_retouch_task_live(p_task) IS TRUE;
$$;
ALTER FUNCTION photo.execution_source_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.execution_source_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) RENAME TO authorize_gateway_task_before_execution_source;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task_before_execution_source(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
CREATE FUNCTION photo.authorize_gateway_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,worker_id uuid,attempt integer,fence bigint,lease_until timestamptz,deadline timestamptz,operation text,execution_profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.authorize_gateway_task_before_execution_source(p_task,p_attempt,p_fence,p_worker,p_certificate);
END $$;
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) TO photo_gateway;
-- Final public claim can wait on registry/operation wrappers after lease creation.
DO $$
DECLARE source text;definition text;marker text:=' END IF;'||chr(10)||' RETURN QUERY SELECT claimed.outcome';
BEGIN
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid='photo.claim_task(uuid,integer,uuid,jsonb)'::regprocedure
 AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,'V2_OPERATION_CLAIM_ACTIVATION_STALE')=0
 OR (length(source)-length(replace(source,marker,'')))/length(marker)<>1 THEN
 RAISE EXCEPTION 'CLAIM_FINAL_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,source,replace(source,marker,$guard$
  -- 02537: a completed wrapper wait must not commit an expired claim.
  IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'TASK_CLAIM_SOURCE_STALE' USING ERRCODE='40001';END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.tasks current_task JOIN photo.resource_slots slot
   ON slot.tenant_id=current_task.tenant_id AND slot.task_id=current_task.id
   WHERE current_task.id=p_task AND current_task.state='running'
   AND current_task.attempt=p_attempt AND current_task.worker_id=p_worker
   AND current_task.fencing_token=claimed.fence AND slot.fencing_token=claimed.fence
   AND current_task.lease_until>clock_timestamp() AND current_task.deadline>clock_timestamp()
   AND slot.lease_until>clock_timestamp()) THEN
   RAISE EXCEPTION 'TASK_CLAIM_LEASE_STALE' USING ERRCODE='40001';END IF;
$guard$||marker));
END $$;
DO $$
DECLARE source text;definition text;target oid;matches integer;
 heartbeat_marker constant text:=' until_time:=';
 result_marker constant text:=' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)';
BEGIN
 SELECT count(*),min(p.oid::bigint)::oid INTO matches,target FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND strpos(p.prosrc,'UPDATE photo.tasks SET lease_until=until_time,progress=p_progress')>0;
 IF matches<>1 THEN RAISE EXCEPTION 'HEARTBEAT_SOURCE_AMBIGUOUS';END IF;
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid=target AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,'t:=photo.lock_task(p_task);')=0
 OR (length(source)-length(replace(source,heartbeat_marker,'')))/length(heartbeat_marker)<>1 THEN
  RAISE EXCEPTION 'HEARTBEAT_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,source,replace(source,heartbeat_marker,$guard$
 -- 02537: live source after canonical locks, immediately before lease write.
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 -- A source predicate may itself wait on a registration row. Never revive an expired lease.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false;RETURN;END IF;
$guard$||heartbeat_marker));
 SELECT count(*),min(p.oid::bigint)::oid INTO matches,target FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND strpos(p.prosrc,result_marker)>0;
 IF matches<>1 THEN RAISE EXCEPTION 'RESULT_SOURCE_AMBIGUOUS';END IF;
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid=target AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR strpos(source,'-- 02535:')=0 THEN RAISE EXCEPTION 'RESULT_SOURCE_FENCE_MISSING';END IF;
 EXECUTE replace(definition,source,replace(source,result_marker,$guard$
 -- 02537: source revocation while waiting outputs refuses canonical publication.
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text;RETURN;END IF;
 -- The source check can wait; final clock checks follow it.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text;RETURN;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(m->'outputs') output
 JOIN photo.artifacts artifact ON artifact.tenant_id=t.tenant_id AND artifact.id=(output->>'artifact_id')::uuid
 WHERE artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
 RAISE EXCEPTION 'OUTPUT_NOT_VERIFIED' USING ERRCODE='22023';END IF;
$guard$||result_marker));
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
