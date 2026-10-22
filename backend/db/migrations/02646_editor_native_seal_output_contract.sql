-- +goose Up
-- A typed seal publishes a flat, independently adoptable evidence inventory.
-- This does not qualify native render or replace physical seal adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_seal_result_outputs_ready(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;b photo.editor_effect_builds%ROWTYPE;
 a photo.artifacts%ROWTYPE;u photo.output_uploads%ROWTYPE;params photo.artifacts%ROWTYPE;
 item jsonb;seen uuid[]:=ARRAY[]::uuid[];roles text[]:=ARRAY[]::text[];
 mandatory text[]:=ARRAY['changed_mask','changed_pixels','executed_base_receipt','image_report','image_result'];
 allowed text[]:=ARRAY['changed_mask','changed_pixels','executed_base_receipt','generated_mask','generated_pixels','image_report','image_result'];
BEGIN
 IF p_tenant IS NULL OR p_task IS NULL OR p_attempt IS NULL OR p_fence IS NULL
 OR p_attempt<1 OR p_fence<1 OR jsonb_typeof(p_outputs) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_outputs) NOT BETWEEN 5 AND 7 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND seal_task_id=p_task AND seal_run_id=t.run_id;
 SELECT * INTO params FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id;
 IF t.id IS NULL OR r.id IS NULL OR b.run_id IS NULL OR params.id IS NULL
 OR b.seal_generative_requested IS NULL OR r.kind<>'v2' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=r.id
  AND w.project_id=b.project_id AND w.workflow_type='provenance' AND w.renderer_version='v2.2')
 OR photo.editor_effect_seal_worker_ready(p_tenant,r.id,t.id,p_attempt,p_fence,t.worker_id,
  t.params_artifact_id,params.sha256,b.seal_plan_params,t.input_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_outputs) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'artifact_id'-'sha256')<>'{}'::jsonb
  OR jsonb_typeof(item->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(item->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(item->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (item->>'artifact_id')::uuid=ANY(seen) THEN RETURN false;END IF;
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(item->>'artifact_id')::uuid;
  SELECT * INTO u FROM photo.output_uploads WHERE tenant_id=p_tenant AND artifact_id=a.id;
  IF a.id IS NULL OR u.id IS NULL OR a.sha256 IS DISTINCT FROM item->>'sha256'
  OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
  OR coalesce(a.object_version,'') IN('','null') OR a.expires_at<=clock_timestamp()
  OR a.created_by_task_id IS DISTINCT FROM t.id OR a.produced_attempt IS DISTINCT FROM p_attempt
  OR a.produced_fence IS DISTINCT FROM p_fence OR NOT a.output_role=ANY(allowed)
  OR a.output_role=ANY(roles) OR u.state<>'ready' OR u.completed_at IS NULL
  OR (u.task_id,u.attempt,u.fence,u.worker_id) IS DISTINCT FROM (t.id,p_attempt,p_fence,t.worker_id)
  OR (u.role,u.bucket,u.object_key,u.object_version,u.sha256,u.bytes,u.media_type)
   IS DISTINCT FROM (a.output_role,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type)
  OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations registration WHERE registration.tenant_id=p_tenant
   AND registration.worker_id=t.worker_id AND registration.enabled AND registration.db_principal=session_user
   AND registration.certificate_sha256=u.certificate_sha256)
  OR NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy
   WHERE policy.operation='editor.effects_seal' AND policy.version='1' AND policy.role=a.output_role
   AND policy.max_count=1 AND policy.media_types ? a.media_type AND a.bytes<=policy.max_bytes)
  THEN RETURN false;END IF;
  seen:=array_append(seen,a.id);roles:=array_append(roles,a.output_role);
 END LOOP;
 IF NOT roles @> mandatory OR ('generated_mask'=ANY(roles)) IS DISTINCT FROM b.seal_generative_requested
 OR ('generated_pixels'=ANY(roles) AND NOT 'generated_mask'=ANY(roles))
 OR (('generated_mask'=ANY(roles) OR 'generated_pixels'=ANY(roles)) AND b.seal_generative_requested IS DISTINCT FROM true)
 OR EXISTS(SELECT 1 FROM photo.output_uploads upload WHERE upload.tenant_id=p_tenant
  AND upload.task_id=p_task AND upload.attempt=p_attempt AND upload.fence=p_fence
  AND upload.state='ready' AND NOT upload.artifact_id=ANY(seen)) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_effect_seal_result_outputs_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_result_outputs_ready(uuid,uuid,integer,bigint,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance,photo_semantic_admission;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE source text;before pg_proc%ROWTYPE;after pg_proc%ROWTYPE;anchor text;branch text; BEGIN
 SELECT * INTO before FROM pg_proc WHERE oid='photo.guard_v2_result_root()'::regprocedure;
 IF encode(sha256(convert_to(before.prosrc,'UTF8')),'hex') IS DISTINCT FROM 'a9c3724f7a1632cd3779f6f3846151a54d093fc989c024b85267dae775e6ca81'
 OR before.prosecdef IS DISTINCT FROM true OR pg_get_userbyid(before.proowner)<>'photo_fn_owner'
 OR NOT coalesce(('search_path=pg_catalog, photo'=ANY(before.proconfig)
  OR 'search_path=pg_catalog,photo'=ANY(before.proconfig)),false)
 THEN RAISE EXCEPTION 'EDITOR_SEAL_RESULT_GUARD_PREIMAGE';END IF;
 anchor:=$anchor$ IF run_row.kind<>'v2' THEN RETURN NEW; END IF;$anchor$;
 branch:=$branch$
 IF task_row.operation='editor.effects_seal' THEN
  IF photo.editor_effect_seal_result_outputs_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'EDITOR_SEAL_TYPED_RESULT_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;$branch$;
 IF (length(before.prosrc)-length(replace(before.prosrc,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'EDITOR_SEAL_RESULT_GUARD_ANCHOR';END IF;
 source:=pg_get_functiondef(before.oid);
 EXECUTE replace(source,anchor,anchor||branch);
 SELECT * INTO after FROM pg_proc WHERE oid=before.oid;
 IF replace(after.prosrc,anchor||branch,anchor) IS DISTINCT FROM before.prosrc

 OR (to_jsonb(after)-'prosrc') IS DISTINCT FROM (to_jsonb(before)-'prosrc')
 THEN RAISE EXCEPTION 'EDITOR_SEAL_RESULT_GUARD_POSTIMAGE';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
