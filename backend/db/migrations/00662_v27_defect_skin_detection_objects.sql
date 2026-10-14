-- +goose Up
-- Exact canonical detector dependency declared by the upstream skin parser.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_skin_detection_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_mask uuid,p_mask_sha text,p_selector text,p_parse uuid,p_parse_sha text,p_detection uuid,p_detection_sha text)
RETURNS TABLE(contract text,id uuid,sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT selected.contract,object.id,object.sha256,object.bucket,object.object_key,object.object_version,object.bytes
 FROM photo.artifacts detection
 JOIN photo.tasks detector ON detector.tenant_id=detection.tenant_id AND detector.id=detection.created_by_task_id
 JOIN photo.runs run_row ON run_row.tenant_id=detector.tenant_id AND run_row.id=detector.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=detector.tenant_id AND result.id=detector.canonical_result_id AND result.task_id=detector.id AND result.attempt=detector.attempt AND result.fencing_token=detector.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=detector.tenant_id AND attempt.task_id=detector.id AND attempt.attempt=detector.attempt AND attempt.fencing_token=detector.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts params ON params.tenant_id=detector.tenant_id AND params.id=detector.params_artifact_id
 JOIN photo.artifacts parse_ref ON parse_ref.tenant_id=detector.tenant_id AND parse_ref.id=p_parse AND parse_ref.sha256=p_parse_sha
 JOIN photo.tasks parser ON parser.tenant_id=parse_ref.tenant_id AND parser.id=parse_ref.created_by_task_id
 CROSS JOIN LATERAL (VALUES('detection',detection.id),('params',params.id)) selected(contract,id)
 JOIN photo.artifacts object ON object.tenant_id=detector.tenant_id AND object.id=selected.id
 WHERE p_selector='skin' AND detection.tenant_id=p_tenant AND detection.id=p_detection AND detection.sha256=p_detection_sha
 AND (SELECT count(*) FROM photo.semantic_proposal_defect_skin_parse_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_mask,p_mask_sha,p_selector,p_parse,p_parse_sha))=3
 AND detector.operation='face.detect' AND detector.operation_version='1' AND detector.state='succeeded' AND detection.output_role='face_boxes'
 AND attempt.backend IN('cpu','cuda','mps') AND detector.execution_profile->>'backend'=attempt.backend AND attempt.profile->>'backend'=attempt.backend
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(detector.inputs)='array' AND detector.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND jsonb_typeof(parser.inputs)='array' AND parser.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha))
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha))
 AND detection.produced_attempt=detector.attempt AND detection.produced_fence=detector.fencing_token
 AND detection.media_type='application/json' AND params.media_type='application/json' AND detection.bytes BETWEEN 2 AND 1048576 AND params.bytes BETWEEN 2 AND 1048576
 AND NOT EXISTS(SELECT 1 FROM (VALUES(detection.state,detection.verified_at,detection.object_version,detection.expires_at),(params.state,params.verified_at,params.object_version,params.expires_at)) objects(state,verified,version,expires)
 WHERE state<>'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
 ORDER BY selected.contract,object.id;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_skin_detection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_skin_detection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_skin_detection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
