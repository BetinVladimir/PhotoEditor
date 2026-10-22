-- Work-only candidate. Actual Apply-v4 source authority; never relabel typed outputs.
-- +goose Up
-- +goose StatementBegin
DO $patch$
DECLARE item record; prior record; after_state jsonb; revised text;
 old_join text:=$old$  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND
   w.run_id=r.id AND w.project_id=p_project$old$;
 new_join text:=$new$  JOIN photo.snapshots selected ON selected.tenant_id=r.tenant_id
   AND selected.id=r.input_snapshot_id AND selected.kind='selection'
   AND selected.principal_type='user' AND selected.principal_id=r.created_by
  JOIN photo.collections source_collection ON source_collection.tenant_id=selected.tenant_id
   AND source_collection.id=selected.collection_id AND source_collection.project_id=p_project$new$;
 old_scope text:=$old$  WHERE r.tenant_id=p_tenant AND r.created_by=e.approved_by AND
   NOT r.cancel_requested AND$old$;
 new_scope text:=$new$  WHERE r.tenant_id=p_tenant AND r.created_by=e.approved_by AND
   r.kind='apply' AND r.failure_policy='continue_items' AND
   r.config_snapshot->>'pipeline'='apply' AND
   r.config_snapshot->'pipeline_version'='4'::jsonb AND
   r.config_snapshot->>'renderer_version'='photo_renderer_v2' AND
   r.config_snapshot->>'failure_policy'='continue_items' AND
   r.config_snapshot->>'input_snapshot_id'=selected.id::text AND
   r.config_snapshot->>'input_snapshot_digest'=selected.digest AND
   r.config_snapshot->>'source_collection_id'=selected.collection_id::text AND
   r.config_snapshot->>'source_revision'=selected.source_revision::text AND
   build.source_collection_id=selected.collection_id AND
   NOT r.cancel_requested AND
   EXISTS(SELECT 1 FROM photo.output_uploads uploaded
    WHERE uploaded.tenant_id=report.tenant_id AND uploaded.artifact_id=report.id
     AND uploaded.task_id=detail.id AND uploaded.attempt=dr.attempt
     AND uploaded.fence=dr.fencing_token AND uploaded.state='ready'
     AND uploaded.role=report.output_role AND uploaded.sha256=report.sha256
     AND uploaded.bytes=report.bytes AND uploaded.bucket=report.bucket
     AND uploaded.object_key=report.object_key AND uploaded.object_version=report.object_version
     AND uploaded.media_type=report.media_type AND uploaded.completed_at IS NOT NULL
     AND EXISTS(SELECT 1 FROM photo.task_attempts receipt_attempt
      WHERE receipt_attempt.tenant_id=uploaded.tenant_id AND receipt_attempt.task_id=uploaded.task_id
       AND receipt_attempt.attempt=uploaded.attempt AND receipt_attempt.fencing_token=uploaded.fence
       AND receipt_attempt.worker_id=uploaded.worker_id AND receipt_attempt.finished_at IS NOT NULL)) AND
   EXISTS(SELECT 1 FROM photo.output_uploads uploaded
    WHERE uploaded.tenant_id=img.tenant_id AND uploaded.artifact_id=img.id
     AND uploaded.task_id=image_task.id AND uploaded.attempt=ir.attempt
     AND uploaded.fence=ir.fencing_token AND uploaded.state='ready'
     AND uploaded.role=img.output_role AND uploaded.sha256=img.sha256
     AND uploaded.bytes=img.bytes AND uploaded.bucket=img.bucket
     AND uploaded.object_key=img.object_key AND uploaded.object_version=img.object_version
     AND uploaded.media_type=img.media_type AND uploaded.completed_at IS NOT NULL
     AND EXISTS(SELECT 1 FROM photo.task_attempts receipt_attempt
      WHERE receipt_attempt.tenant_id=uploaded.tenant_id AND receipt_attempt.task_id=uploaded.task_id
       AND receipt_attempt.attempt=uploaded.attempt AND receipt_attempt.fencing_token=uploaded.fence
       AND receipt_attempt.worker_id=uploaded.worker_id AND receipt_attempt.finished_at IS NOT NULL)) AND
   EXISTS(SELECT 1 FROM photo.output_uploads uploaded
    WHERE uploaded.tenant_id=resolved.tenant_id AND uploaded.artifact_id=resolved.id
     AND uploaded.task_id=resolved_task.id AND uploaded.attempt=rr.attempt
     AND uploaded.fence=rr.fencing_token AND uploaded.state='ready'
     AND uploaded.role=resolved.output_role AND uploaded.sha256=resolved.sha256
     AND uploaded.bytes=resolved.bytes AND uploaded.bucket=resolved.bucket
     AND uploaded.object_key=resolved.object_key AND uploaded.object_version=resolved.object_version
     AND uploaded.media_type=resolved.media_type AND uploaded.completed_at IS NOT NULL
     AND EXISTS(SELECT 1 FROM photo.task_attempts receipt_attempt
      WHERE receipt_attempt.tenant_id=uploaded.tenant_id AND receipt_attempt.task_id=uploaded.task_id
       AND receipt_attempt.attempt=uploaded.attempt AND receipt_attempt.fencing_token=uploaded.fence
       AND receipt_attempt.worker_id=uploaded.worker_id AND receipt_attempt.finished_at IS NOT NULL)) AND$new$;
BEGIN
 FOR item IN SELECT * FROM (VALUES
 (1,'photo.v23_portrait_detail_event_producer(uuid,uuid,uuid,uuid,uuid)','fc559d7fe350df97c20cbbb40682f39466defbe4acc48908b8d3268091352bbb',true),
 (2,'photo.v23_portrait_detail_event_producer_evaluate_core(uuid,uuid,uuid,uuid,uuid)','32a78bd3f6630a0045a4aacb35ea41feebe6e624af5281eba3ecbcc21ca31823',false)) AS changes(ordinal,signature,source_sha,scheduler_public) ORDER BY ordinal LOOP
  SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,to_jsonb(p)-'prosrc' metadata
   INTO prior FROM pg_proc p
   WHERE p.oid=to_regprocedure(item.signature)
    AND p.proowner='photo_fn_owner'::regrole AND p.prosecdef AND p.prokind='f'
    AND p.proretset AND p.prorettype='record'::regtype AND p.pronargs=5
    AND p.proargtypes='2950 2950 2950 2950 2950'::oidvector
    AND p.proargnames[1:5]=ARRAY['p_tenant','p_project','p_actor','p_auth_session','p_event']::text[]
    AND pg_get_function_result(p.oid)='TABLE(asset_version_id uuid, source_id uuid, source_sha text, consent_id uuid, consent_epoch bigint, run_id uuid, task_id uuid, result_id uuid, attempt_id uuid, image_id uuid, image_sha text, params_id uuid, params_sha text, resolved_id uuid, resolved_sha text)'
    AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
    AND p.provolatile='v' AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
    AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=item.source_sha;
  IF NOT FOUND THEN RAISE EXCEPTION 'Unexpected V23 detail producer source or authority: %',item.signature; END IF;
  IF EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
     WHERE p.oid=prior.oid AND a.privilege_type='EXECUTE'
      AND a.grantee<>'photo_fn_owner'::regrole AND NOT(item.scheduler_public AND a.grantee='photo_scheduler'::regrole))
   OR has_function_privilege('photo_scheduler',prior.oid,'EXECUTE') IS DISTINCT FROM item.scheduler_public
   OR EXISTS(SELECT 1 FROM pg_roles r WHERE r.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal','photo_maintenance','photo_auth','photo_publisher')
     AND has_function_privilege(r.oid,prior.oid,'EXECUTE')) THEN
   RAISE EXCEPTION 'Unexpected effective V23 detail producer EXEC: %',item.signature; END IF;
  IF (length(prior.prosrc)-length(replace(prior.prosrc,old_join,'')))/length(old_join)<>1
   OR (length(prior.prosrc)-length(replace(prior.prosrc,old_scope,'')))/length(old_scope)<>1 THEN
   RAISE EXCEPTION 'Unexpected V23 detail producer domain shape: %',item.signature; END IF;
  revised:=replace(replace(prior.definition,old_join,new_join),old_scope,new_scope);
  EXECUTE revised;
  SELECT to_jsonb(p)-'prosrc' INTO after_state FROM pg_proc p WHERE p.oid=prior.oid;
  IF after_state IS DISTINCT FROM prior.metadata OR
   (SELECT p.prosrc FROM pg_proc p WHERE p.oid=prior.oid)
    IS DISTINCT FROM replace(replace(prior.prosrc,old_join,new_join),old_scope,new_scope) THEN
   RAISE EXCEPTION 'V23 detail producer identity/authority changed: %',item.signature; END IF;
 END LOOP;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
