-- +goose Up
-- +goose StatementBegin
-- Existing source/metadata/version fences stay in place; new closed roles still
-- require entry-bound application producer qualification and physical checks.
DO $$ DECLARE definition text;anchor text:='''AbsoluteLUTArray''';BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)'::regprocedure);
 IF definition IS NULL OR strpos(definition,'SECURITY DEFINER')=0 OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>3 THEN RAISE EXCEPTION 'HISTORICAL_PREPARED_SCOPE_SHAPE';END IF;
 definition:=replace(definition,anchor,anchor||',''WorkingPixels'',''WorkingAlpha''');
 anchor:='''DustCandidates''';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'HISTORICAL_JSON_SCOPE_SHAPE';END IF;
 definition:=replace(definition,anchor,anchor||',''WorkingImage'',''FaceLandmarks'',''FaceBoxes''');EXECUTE definition;
END $$;
CREATE FUNCTION photo.v30_interchange_prepared_ref(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_contract text,p_original uuid,p_original_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;rid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND artifact_id=p_artifact AND owner_id=p_artifact AND purpose='v30_import_prepared' AND state='ready' FOR SHARE;
 IF NOT FOUND OR d.v30_import_prepared#>>'{origin,artifact_id}' IS DISTINCT FROM p_original::text OR d.v30_import_prepared#>>'{origin,sha256}' IS DISTINCT FROM p_original_sha THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_artifact,d.sha256,p_contract);
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',p_artifact,'sha256',d.sha256);
END $$;
CREATE FUNCTION photo.v30_import_historical_document_authorized(p_tenant uuid,p_workflow uuid,p_parsed text,p_current uuid,p_current_sha text,p_contract text,p_document uuid,p_document_sha text,p_image uuid,p_image_sha text,p_model text,p_backend text,p_boxes uuid,p_boxes_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;project uuid;operation text;role text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);IF sources IS NULL THEN RETURN false;END IF;project:=(sources->>'project_id')::uuid;
 IF NOT photo.v30_canonical_image_ancestor(p_tenant,project,p_current,p_current_sha,p_image,p_image_sha) THEN RETURN false;END IF;
 IF p_contract='WorkingImage' THEN RETURN p_document=p_image AND p_document_sha=p_image_sha;END IF;
 IF p_contract='FaceLandmarks' THEN operation:='face.landmarks';role:='face_landmarks';
 ELSIF p_contract='FaceBoxes' THEN operation:='face.detect';role:='face_boxes';ELSE RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.id=t.canonical_result_id AND r.task_id=t.id AND r.attempt=t.attempt AND r.fencing_token=t.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=t.tenant_id AND origin.id=t.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
 JOIN photo.v2_qualification_policies policy ON policy.operation=t.operation AND policy.operation_version=t.operation_version AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
 JOIN photo.model_qualifications q ON q.tenant_id=a.tenant_id AND q.project_id=project AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha AND q.backend=policy.backend
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 WHERE a.tenant_id=p_tenant AND a.id=p_document AND a.sha256=p_document_sha AND a.output_role=role AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND t.operation=operation AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested AND NOT origin.cancel_requested AND origin.status IN('completed','completed_with_errors')
 AND (workflow.project_id=project OR origin.config_snapshot->>'project_id'=project::text)
 AND a.produced_attempt=r.attempt AND a.produced_fence=r.fencing_token AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
 AND (p_contract<>'FaceLandmarks' OR t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_boxes,'sha256',p_boxes_sha)))
 AND policy.model_lock_sha=p_model AND policy.backend=p_backend AND t.execution_profile->>'model_lock_id'=p_model AND t.execution_profile->>'backend'=p_backend
 AND q.status='passed' AND q.trusted_import AND EXISTS(SELECT FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_ref(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v30_import_historical_document_authorized(uuid,uuid,text,uuid,text,text,uuid,text,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_ref(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text),photo.v30_import_historical_document_authorized(uuid,uuid,text,uuid,text,text,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_ref(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text),photo.v30_import_historical_document_authorized(uuid,uuid,text,uuid,text,text,uuid,text,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
