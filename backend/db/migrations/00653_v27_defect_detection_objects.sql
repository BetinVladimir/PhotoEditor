-- +goose Up
-- Canonical source-scoped detector declared by the landmark producer.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_detection_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT detection.bucket,detection.object_key,detection.object_version,detection.bytes
 FROM photo.artifacts detection
 JOIN photo.tasks producer ON producer.tenant_id=detection.tenant_id AND producer.id=detection.created_by_task_id
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts landmark ON landmark.tenant_id=detection.tenant_id AND landmark.id=p_landmark
 JOIN photo.tasks landmark_producer ON landmark_producer.tenant_id=landmark.tenant_id AND landmark_producer.id=landmark.created_by_task_id
 WHERE EXISTS(SELECT 1 FROM photo.semantic_proposal_defect_landmark_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_landmark,p_landmark_sha))
 AND detection.tenant_id=p_tenant AND detection.id=p_detection AND detection.sha256=p_detection_sha
 AND detection.output_role='face_boxes' AND detection.state='ready' AND detection.verified_at IS NOT NULL
 AND detection.object_version IS NOT NULL AND detection.object_version NOT IN('','null') AND (detection.expires_at IS NULL OR detection.expires_at>clock_timestamp())
 AND detection.media_type='application/json' AND detection.bytes BETWEEN 2 AND 1048576
 AND producer.operation='face.detect' AND producer.operation_version='1' AND producer.state='succeeded'
 AND detection.produced_attempt=producer.attempt AND detection.produced_fence=producer.fencing_token
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha))
 AND jsonb_typeof(landmark_producer.inputs)='array' AND landmark_producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_detection,'sha256',p_detection_sha));
$$;
ALTER FUNCTION photo.semantic_proposal_defect_detection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_detection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_detection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
