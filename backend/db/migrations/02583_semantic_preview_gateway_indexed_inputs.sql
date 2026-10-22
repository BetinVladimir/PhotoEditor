-- +goose Up
-- Extend the innermost indexed-input selector. Every existing outer revocation
-- wrapper remains in the call chain; this does not introduce a fallback grant.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preview_indexed_input_ready(p_tenant uuid,p_task uuid,p_run uuid,p_artifact uuid,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;project uuid;expected numeric;closure_params jsonb;family text;target uuid;preview_key text;receipt_key text;selected_receipt jsonb;approved_ref jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run;
 IF t.id IS NULL OR t.state IS DISTINCT FROM 'running' OR t.cancel_requested
 OR t.operation_version IS DISTINCT FROM '1' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.v2_enabled)
 THEN RETURN false;END IF;
 CASE t.operation
 WHEN 'semantic.preview_local_exposure' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 WHEN 'semantic.preview_remove_object' THEN IF t.resource_class<>'gpu' THEN RETURN false;END IF;
 WHEN 'semantic.preview_background_cleanup' THEN IF t.resource_class<>'gpu' THEN RETURN false;END IF;
 WHEN 'semantic.preview_retouch_region' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_exposure' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_removal' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_background_cleanup' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_retouch_region' THEN IF t.resource_class<>'cpu' THEN RETURN false;END IF;
 ELSE RETURN false;
 END CASE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF r.id IS NULL OR r.status NOT IN('queued','running') OR NOT r.graph_sealed OR r.cancel_requested
 OR (t.operation LIKE 'semantic.preview_%' AND r.kind IS DISTINCT FROM 'preview')
 OR (t.operation LIKE 'semantic.adopt_shown_%' AND r.kind IS DISTINCT FROM 'apply')
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 OR photo.execution_source_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT i.project_id INTO project FROM photo.v2_task_inputs i JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 JOIN photo.projects p ON p.tenant_id=i.tenant_id AND p.id=i.project_id AND p.archived_at IS NULL
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=p_artifact AND i.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF project IS NULL THEN RETURN false;END IF;
 IF t.operation LIKE 'semantic.adopt_shown_%' THEN
  -- This is a closure-check projection, not reconstructed/raw Params. These
  -- predicates consume only identity plus five registered root references.
  family:=CASE t.operation WHEN 'semantic.adopt_shown_exposure' THEN 'Exposure'
   WHEN 'semantic.adopt_shown_removal' THEN 'Removal'
   WHEN 'semantic.adopt_shown_background_cleanup' THEN 'BackgroundCleanup'
   WHEN 'semantic.adopt_shown_retouch_region' THEN 'Retouch' END;
  SELECT (node->>'target_id')::uuid INTO STRICT target FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node
   WHERE node->>'task_id'=t.id::text AND node->>'params_artifact_id'=t.params_artifact_id::text;
  preview_key:=CASE t.operation WHEN 'semantic.adopt_shown_exposure' THEN 'semantic_preapply_preview'
   WHEN 'semantic.adopt_shown_removal' THEN 'semantic_removal_preview'
   WHEN 'semantic.adopt_shown_background_cleanup' THEN 'semantic_background_cleanup_preview'
   WHEN 'semantic.adopt_shown_retouch_region' THEN 'semantic_retouch_preview' END;
  receipt_key:=CASE t.operation WHEN 'semantic.adopt_shown_exposure' THEN 'v27_preapply_preview_receipt'
   WHEN 'semantic.adopt_shown_removal' THEN 'v27_removal_preview_receipt'
   WHEN 'semantic.adopt_shown_background_cleanup' THEN 'v27_background_cleanup_preview_receipt'
   WHEN 'semantic.adopt_shown_retouch_region' THEN 'v27_retouch_preview_receipt' END;
  -- WorkingImage/NpyArray contracts also occur in source dependencies. Select
  -- output refs from the exact canonical preview receipt, never by first type.
  SELECT to_jsonb(preview)->receipt_key INTO STRICT selected_receipt FROM photo.runs preview
   JOIN photo.tasks producer ON producer.tenant_id=preview.tenant_id AND producer.run_id=preview.id
    AND producer.id::text=to_jsonb(preview)->receipt_key->>'task_id'
   WHERE preview.tenant_id=p_tenant AND preview.kind='preview' AND preview.graph_sealed AND NOT preview.cancel_requested
   AND preview.created_by=r.created_by AND preview.config_snapshot->preview_key->'params'->>'target_id'=target::text
   AND EXISTS(SELECT 1 FROM photo.semantic_targets target_row JOIN photo.v2_review_batches batch ON batch.tenant_id=target_row.tenant_id AND batch.id=target_row.batch_id
    WHERE target_row.tenant_id=p_tenant AND target_row.id=target AND target_row.project_id=project AND target_row.child_run_id=p_run
    AND target_row.state='queued' AND batch.child_run_id=p_run AND batch.actor_id=r.created_by AND batch.action='apply')
   AND producer.state='succeeded' AND NOT producer.cancel_requested
   AND producer.canonical_result_id::text=to_jsonb(preview)->receipt_key->>'result_id'
   AND producer.attempt::text=to_jsonb(preview)->receipt_key->>'attempt'
   AND producer.fencing_token::text=to_jsonb(preview)->receipt_key->>'fence'
   AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['presentation','numerical_evidence','working_image','pixels']) field
    WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project
     AND i.artifact_id::text=to_jsonb(preview)->receipt_key->field->>'artifact_id'
     AND i.sha256=to_jsonb(preview)->receipt_key->field->>'sha256'));
  SELECT jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) INTO STRICT approved_ref
   FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.project_id=project AND i.contract='ApprovedSemanticEdit';
  closure_params:=jsonb_build_object('tenant_id',p_tenant,'child_run_id',p_run,'target_id',target,
   'approved_edit',approved_ref,'presentation',selected_receipt->'presentation','numerical_evidence',selected_receipt->'numerical_evidence',
   'working_image',selected_receipt->'working_image','pixels',selected_receipt->'pixels');
  CASE t.operation
   WHEN 'semantic.adopt_shown_exposure' THEN IF photo.semantic_shown_exposure_closure_ready(t.id,closure_params) IS DISTINCT FROM true THEN RETURN false;END IF;
   WHEN 'semantic.adopt_shown_removal' THEN IF photo.semantic_shown_removal_closure_ready(t.id,closure_params) IS DISTINCT FROM true THEN RETURN false;END IF;
   WHEN 'semantic.adopt_shown_background_cleanup' THEN IF photo.semantic_shown_background_cleanup_closure_ready(t.id,closure_params) IS DISTINCT FROM true THEN RETURN false;END IF;
   WHEN 'semantic.adopt_shown_retouch_region' THEN IF photo.semantic_shown_retouch_closure_ready(t.id,closure_params) IS DISTINCT FROM true THEN RETURN false;END IF;
   ELSE RETURN false;
  END CASE;
 END IF;
 CASE t.operation
 WHEN 'semantic.preview_local_exposure' THEN
  IF photo.semantic_preapply_preview_closure_ready(t.id) IS DISTINCT FROM true
  OR photo.semantic_preapply_preview_run_current(p_tenant,r.id) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.semantic_intents intent WHERE intent.tenant_id=p_tenant AND intent.project_id=project
   AND intent.id::text=r.config_snapshot->'semantic_preapply_preview'->'params'->>'intent_id')
  OR photo.v27_downstream_profile_ready(p_tenant,project,'semantic.local_exposure','1','cpu',r.v27_preapply_preview_profile) IS DISTINCT FROM true
  THEN RETURN false;END IF;
  expected:=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::numeric;
  IF expected IS NULL OR expected NOT BETWEEN 1 AND 9007199254740991 OR NOT EXISTS(
   SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=r.id
   AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
   AND q.bytes::numeric+coalesce((SELECT sum(u.bytes)::numeric FROM photo.output_uploads u JOIN photo.tasks allocated
    ON allocated.tenant_id=u.tenant_id AND allocated.id=u.task_id WHERE u.tenant_id=q.tenant_id AND u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation AND allocated.run_id=r.id),0)=expected
   AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 WHEN 'semantic.preview_remove_object' THEN
  IF photo.semantic_removal_preview_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.preview_background_cleanup' THEN
  IF photo.semantic_background_cleanup_preview_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.preview_retouch_region' THEN
  IF photo.semantic_retouch_preview_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_exposure' THEN
  IF r.config_snapshot->>'project_id' IS DISTINCT FROM project::text OR photo.semantic_shown_exposure_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_removal' THEN
  IF r.config_snapshot->>'project_id' IS DISTINCT FROM project::text OR photo.semantic_shown_removal_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_background_cleanup' THEN
  IF r.config_snapshot->>'project_id' IS DISTINCT FROM project::text OR photo.semantic_shown_background_cleanup_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 WHEN 'semantic.adopt_shown_retouch_region' THEN
  IF r.config_snapshot->>'project_id' IS DISTINCT FROM project::text OR photo.semantic_shown_retouch_task_live(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 ELSE RETURN false;
 END CASE;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR no_data_found OR too_many_rows THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_preview_indexed_input_ready(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preview_indexed_input_ready(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $patch$
DECLARE prior record;revised text;
 needle text := E'  OR (\n   t.execution_profile->>''renderer_version''=''v2.2''';
 replacement text := E'  OR photo.semantic_preview_indexed_input_ready(t.tenant_id,t.id,t.run_id,a.id,a.sha256)\n  OR (\n   t.execution_profile->>''renderer_version''=''v2.2''';
BEGIN
 SELECT p.oid,p.prosrc,pg_get_functiondef(p.oid) definition,p.proowner,p.proacl,p.proconfig,p.provolatile,p.proallargtypes,p.proargmodes,p.proargnames INTO prior
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.gateway_input_before_render_cache(uuid,integer,bigint,uuid,text,uuid)')
 AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
 AND p.prokind='f' AND p.proretset AND p.prorettype='record'::regtype
 AND p.proargtypes='2950 23 20 2950 25 2950'::oidvector
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo'];
 IF prior.oid IS NULL OR (length(prior.prosrc)-length(replace(prior.prosrc,needle,'')))/length(needle)<>1
 OR strpos(prior.prosrc,'semantic_preview_indexed_input_ready')>0 THEN RAISE EXCEPTION 'SEMANTIC_GATEWAY_INDEX_SOURCE_SHAPE';END IF;
 IF EXISTS(SELECT 1 FROM aclexplode(coalesce(prior.proacl,acldefault('f',prior.proowner))) a WHERE a.privilege_type='EXECUTE' AND a.grantee<>prior.proowner)
 OR EXISTS(SELECT 1 FROM pg_roles role WHERE NOT role.rolsuper AND role.oid<>prior.proowner
  AND NOT pg_has_role(role.oid,prior.proowner,'USAGE') AND has_function_privilege(role.oid,prior.oid,'EXECUTE'))
 THEN RAISE EXCEPTION 'SEMANTIC_GATEWAY_INDEX_HELPER_EXPOSED';END IF;
 revised:=replace(prior.prosrc,needle,replacement);EXECUTE replace(prior.definition,prior.prosrc,revised);
 IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=prior.oid AND p.prosrc=revised AND p.proowner=prior.proowner
 AND p.proacl IS NOT DISTINCT FROM prior.proacl AND p.proconfig IS NOT DISTINCT FROM prior.proconfig AND p.prosecdef
 AND p.provolatile=prior.provolatile AND p.proallargtypes IS NOT DISTINCT FROM prior.proallargtypes
 AND p.proargmodes IS NOT DISTINCT FROM prior.proargmodes AND p.proargnames IS NOT DISTINCT FROM prior.proargnames)
 THEN RAISE EXCEPTION 'SEMANTIC_GATEWAY_INDEX_AUTHORITY_CHANGED';END IF;
END $patch$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
