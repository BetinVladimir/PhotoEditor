-- +goose Up
-- Canonical result must belong to a finished attempt with consistent backend.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_proposal_defect_origin(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.asset_versions version JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.projects project ON project.tenant_id=asset.tenant_id AND project.id=asset.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=version.tenant_id AND tenant.status='active'
 WHERE version.tenant_id=p_tenant AND version.id=p_version AND version.review_state='accepted' AND version.master_artifact_id=p_source AND version.content_hash=p_source_sha
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified')
 AND EXISTS(
 SELECT 1 FROM photo.artifacts proposal
 JOIN photo.tasks producer ON producer.tenant_id=proposal.tenant_id AND producer.id=proposal.created_by_task_id
 JOIN photo.task_attempts completed_attempt ON completed_attempt.tenant_id=producer.tenant_id AND completed_attempt.task_id=producer.id AND completed_attempt.attempt=producer.attempt AND completed_attempt.fencing_token=producer.fencing_token AND completed_attempt.finished_at IS NOT NULL AND completed_attempt.backend IN('cpu','cuda','mps') AND producer.execution_profile->>'backend'=completed_attempt.backend AND completed_attempt.profile->>'backend'=completed_attempt.backend AND completed_attempt.backend='cpu'
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.id=p_report AND report.sha256=p_report_sha AND report.created_by_task_id=producer.id AND report.output_role='defect_propose_report'
 JOIN photo.artifacts labels ON labels.tenant_id=producer.tenant_id AND labels.id=p_labels AND labels.sha256=p_labels_sha AND labels.created_by_task_id=producer.id AND labels.output_role='defect_labels'
 JOIN photo.artifacts protection ON protection.tenant_id=producer.tenant_id AND protection.id=p_protection AND protection.sha256=p_protection_sha AND protection.created_by_task_id=producer.id AND protection.output_role='defect_protection'
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id AND params.id=p_params AND params.sha256=p_params_sha
 JOIN photo.artifacts source ON source.tenant_id=producer.tenant_id AND source.id=p_source AND source.sha256=p_source_sha
 WHERE proposal.tenant_id=p_tenant AND proposal.id=p_proposal AND proposal.sha256=p_proposal_sha AND proposal.output_role='defect_candidates'
 AND producer.operation='defects.propose' AND producer.operation_version='1' AND producer.resource_class='cpu' AND producer.state='succeeded'
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_proposal,'sha256',p_proposal_sha),jsonb_build_object('artifact_id',p_report,'sha256',p_report_sha),jsonb_build_object('artifact_id',p_labels,'sha256',p_labels_sha),jsonb_build_object('artifact_id',p_protection,'sha256',p_protection_sha))
 AND NOT EXISTS(SELECT 1 FROM (VALUES(proposal.state,proposal.verified_at,proposal.object_version,proposal.expires_at),(report.state,report.verified_at,report.object_version,report.expires_at),(labels.state,labels.verified_at,labels.object_version,labels.expires_at),(protection.state,protection.verified_at,protection.object_version,protection.expires_at),(params.state,params.verified_at,params.object_version,params.expires_at),(source.state,source.verified_at,source.object_version,source.expires_at)) objects(state,verified,version,expires)
 WHERE state<>'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
 AND proposal.media_type='application/json' AND report.media_type='application/json' AND params.media_type='application/json'
 AND proposal.bytes BETWEEN 2 AND 1048576 AND report.bytes BETWEEN 2 AND 1048576 AND params.bytes BETWEEN 2 AND 1048576
 AND labels.media_type='application/octet-stream' AND protection.media_type='application/octet-stream' AND labels.bytes>=32 AND protection.bytes>=32
 AND NOT EXISTS(SELECT 1 FROM (VALUES(proposal.produced_attempt,proposal.produced_fence),(report.produced_attempt,report.produced_fence),(labels.produced_attempt,labels.produced_fence),(protection.produced_attempt,protection.produced_fence)) fences(attempt,fence)
 WHERE attempt IS DISTINCT FROM producer.attempt OR fence IS DISTINCT FROM producer.fencing_token)
 ),false);
$$;
ALTER FUNCTION photo.semantic_proposal_defect_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_proposal_defect_landmark_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_landmark uuid,p_landmark_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT artifact.bucket,artifact.object_key,artifact.object_version,artifact.bytes
 FROM photo.artifacts artifact
 JOIN photo.tasks producer ON producer.tenant_id=artifact.tenant_id AND producer.id=artifact.created_by_task_id
 JOIN photo.task_attempts completed_attempt ON completed_attempt.tenant_id=producer.tenant_id AND completed_attempt.task_id=producer.id AND completed_attempt.attempt=producer.attempt AND completed_attempt.fencing_token=producer.fencing_token AND completed_attempt.finished_at IS NOT NULL AND completed_attempt.backend IN('cpu','cuda','mps') AND producer.execution_profile->>'backend'=completed_attempt.backend AND completed_attempt.profile->>'backend'=completed_attempt.backend
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_proposal_defect_detection_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT detection.bucket,detection.object_key,detection.object_version,detection.bytes
 FROM photo.artifacts detection
 JOIN photo.tasks producer ON producer.tenant_id=detection.tenant_id AND producer.id=detection.created_by_task_id
 JOIN photo.task_attempts completed_attempt ON completed_attempt.tenant_id=producer.tenant_id AND completed_attempt.task_id=producer.id AND completed_attempt.attempt=producer.attempt AND completed_attempt.fencing_token=producer.fencing_token AND completed_attempt.finished_at IS NOT NULL AND completed_attempt.backend IN('cpu','cuda','mps') AND producer.execution_profile->>'backend'=completed_attempt.backend AND completed_attempt.profile->>'backend'=completed_attempt.backend
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
