-- +goose Up
-- Coordinates and complete acceptance policy are exposed only through the
-- live leased retouch preview task. No object IO occurs inside this statement.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_retouch_preview_report_evidence(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text,p_context uuid,p_context_sha text,p_context_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE; context_doc jsonb;config jsonb; context_ref jsonb;face jsonb;required record;
BEGIN
 IF photo.admit_semantic_retouch_preview(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF p_context IS NULL OR p_context='00000000-0000-0000-0000-000000000000'::uuid
 OR p_context_sha IS NULL OR p_context_sha !~ '^[0-9a-f]{64}$'
 OR p_context_body IS NULL OR octet_length(p_context_body) NOT BETWEEN 2 AND 1048576
 OR encode(public.digest(convert_to(p_context_body,'UTF8'),'sha256'),'hex')<>p_context_sha THEN RETURN NULL;END IF;
 context_doc:=p_context_body::jsonb;context_ref:=jsonb_build_object('artifact_id',p_context,'sha256',p_context_sha);
 SELECT config_snapshot->'semantic_retouch_preview' INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF config->'adapter_context_ref' IS DISTINCT FROM context_ref OR config->'adapter_context' IS DISTINCT FROM context_doc
 OR (SELECT execution_profile->'retouch_context' FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task) IS DISTINCT FROM context_ref
 OR context_doc->>'algorithm' IS DISTINCT FROM 'beautyfilter_v1'
 OR context_doc->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR context_doc->'source' IS DISTINCT FROM p_body::jsonb->'source'
 OR context_doc->'candidate' IS DISTINCT FROM p_body::jsonb->'candidate'
 OR context_doc->'protection' IS DISTINCT FROM p_body::jsonb->'protection'
 OR context_doc->>'model_lock_sha' IS DISTINCT FROM p_body::jsonb->>'model_lock_sha'
 OR context_doc->>'runtime_lock_sha' IS DISTINCT FROM p_body::jsonb->>'runtime_lock_sha'
 OR context_doc->>'device_profile_sha' IS DISTINCT FROM p_body::jsonb->>'device_profile_sha'
 OR jsonb_typeof(context_doc->'faces') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF jsonb_array_length(context_doc->'faces') NOT BETWEEN 1 AND 64 THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=p_context AND a.sha256=p_context_sha AND i.task_id=p_task
 AND i.sha256=p_context_sha AND i.contract='RetouchPreviewAdapterContext'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes=octet_length(p_context_body) AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND THEN RETURN NULL;END IF;
 FOR required IN SELECT context_doc->'transferred_mask' AS ref,'MaskRef'::text AS contract LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id::text=required.ref->>'artifact_id' AND i.sha256=required.ref->>'sha256'
   AND i.contract=required.contract) THEN RETURN NULL;END IF;
 END LOOP;
 FOR face IN SELECT value FROM jsonb_array_elements(context_doc->'faces') LOOP
  IF face->'manual_mode' IS DISTINCT FROM 'true'::jsonb OR jsonb_typeof(face->'manual') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id::text=face->'landmarks'->>'artifact_id' AND i.sha256=face->'landmarks'->>'sha256'
   AND i.contract=CASE WHEN face->>'object_id' IS NULL THEN 'Face68' ELSE 'FaceLandmarks' END)
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id::text=face->'manual'->'artifact'->>'artifact_id' AND i.sha256=face->'manual'->'artifact'->>'sha256'
   AND i.contract='NpyArray') THEN RETURN NULL;END IF;
  -- Optional skin input is still part of the frozen task inventory. A foreign
  -- or malformed non-null ref must not bypass the same gate as manual input.
  IF face->'skin' IS NOT NULL AND face->'skin'<>'null'::jsonb THEN
   IF jsonb_typeof(face->'skin') IS DISTINCT FROM 'object' OR NOT EXISTS(
    SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
    AND i.artifact_id::text=face->'skin'->'artifact'->>'artifact_id'
    AND i.sha256=face->'skin'->'artifact'->>'sha256' AND i.contract='NpyArray')
   THEN RETURN NULL;END IF;
  END IF;
 END LOOP;
 SELECT t.execution_profile->'qualification' INTO pin
 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task;
 IF photo.v27_downstream_profile_ready(p_tenant,
 (SELECT project_id FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=(p_body::jsonb->>'intent_id')::uuid),
 'semantic.preview_retouch_region','1','cpu',
 (SELECT execution_profile FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF pin IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 RETURN jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text) FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
