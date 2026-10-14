-- +goose Up
-- Exact-source canonical landmark input declared by the defect producer.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_landmark_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_landmark uuid,p_landmark_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT artifact.bucket,artifact.object_key,artifact.object_version,artifact.bytes
 FROM photo.artifacts artifact
 JOIN photo.tasks producer ON producer.tenant_id=artifact.tenant_id AND producer.id=artifact.created_by_task_id
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts proposal ON proposal.tenant_id=artifact.tenant_id AND proposal.id=p_proposal
 JOIN photo.tasks defect ON defect.tenant_id=proposal.tenant_id AND defect.id=proposal.created_by_task_id
 WHERE photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha)
 AND artifact.tenant_id=p_tenant AND artifact.id=p_landmark AND artifact.sha256=p_landmark_sha
 AND artifact.output_role='face_landmarks' AND artifact.state='ready' AND artifact.verified_at IS NOT NULL
 AND artifact.object_version IS NOT NULL AND artifact.object_version NOT IN('','null') AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
 AND artifact.media_type='application/json' AND artifact.bytes BETWEEN 2 AND 1048576
 AND producer.operation='face.landmarks' AND producer.operation_version='1' AND producer.state='succeeded'
 AND artifact.produced_attempt=producer.attempt AND artifact.produced_fence=producer.fencing_token
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_landmark,'sha256',p_landmark_sha))
 AND jsonb_typeof(defect.inputs)='array' AND defect.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_landmark,'sha256',p_landmark_sha));
$$;
ALTER FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
