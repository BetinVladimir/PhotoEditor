-- +goose Up
-- Canonical native producers consume their immutable registered V2 closure.
-- Original direct-input producers retain the same declaration contract.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_declares_input(p_tenant uuid,p_task uuid,p_artifact uuid,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='defects.propose' AND t.operation_version='1'
 AND ((jsonb_typeof(t.inputs)='array' AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)))
 OR (t.state='succeeded' AND NOT t.cancel_requested AND t.execution_profile->>'renderer_version'='v2.2' AND photo.v2_input_closure_ready(t.id)
 AND EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.v2_task_inputs i ON i.tenant_id=w.tenant_id AND i.project_id=w.project_id AND i.task_id=t.id
 WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.status IN('running','waiting_review','completed') AND w.workflow_type='semantic' AND w.renderer_version='v2.2'
 AND r.config_snapshot->'v27_defect_prepare'->'schema_version'='1'::jsonb
 AND r.config_snapshot->'v27_defect_prepare'->>'task_id'=t.id::text
 AND r.config_snapshot->'v27_defect_prepare'->>'input_digest'=t.input_digest
 AND i.artifact_id=p_artifact AND i.sha256=p_sha AND i.contract IN('Params','WorkingImage','NpyArray','FaceLandmarks','MaskRef')))));
$$;
ALTER FUNCTION photo.semantic_defect_declares_input(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_declares_input(uuid,uuid,uuid,text) FROM PUBLIC;
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
 AND photo.semantic_defect_declares_input(p_tenant,defect.id,p_landmark,p_landmark_sha);
$$;
ALTER FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_landmark_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;

CREATE OR REPLACE FUNCTION photo.semantic_proposal_defect_face_mask_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_mask uuid,p_mask_sha text,p_selector text)
RETURNS TABLE(root_id uuid,root_sha text,root_bucket text,root_key text,root_version text,root_bytes bigint,params_id uuid,params_sha text,params_bucket text,params_key text,params_version text,params_bytes bigint,pixel_bucket text,pixel_key text,pixel_version text,pixel_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT root.id,root.sha256,root.bucket,root.object_key,root.object_version,root.bytes,
 params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.bytes,
 pixel.bucket,pixel.object_key,pixel.object_version,pixel.bytes
 FROM photo.artifacts pixel
 JOIN photo.tasks producer ON producer.tenant_id=pixel.tenant_id AND producer.id=pixel.created_by_task_id
 JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 JOIN photo.artifacts proposal ON proposal.tenant_id=pixel.tenant_id AND proposal.id=p_proposal
 JOIN photo.tasks defect ON defect.tenant_id=proposal.tenant_id AND defect.id=proposal.created_by_task_id
 WHERE photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha)
 AND pixel.tenant_id=p_tenant AND pixel.id=p_mask AND pixel.sha256=p_mask_sha
 AND producer.state='succeeded' AND producer.operation_version='1' AND producer.resource_class='cpu'
 AND ((p_selector='manual' AND producer.operation='mask.rasterize' AND pixel.output_role='mask_target' AND root.output_role='rasterized_mask') OR (p_selector='skin' AND producer.operation='mask.face_skin' AND pixel.output_role='skin_mask' AND root.output_role='skin_mask_ref'))
 AND producer.execution_profile->>'backend'='cpu' AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu'
 AND (workflow.project_id=p_project OR run_row.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(producer.inputs)='array'
 AND ((p_selector='manual' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha)))
 OR (p_selector='skin' AND EXISTS(
 SELECT 1 FROM photo.artifacts parse_ref
 JOIN photo.tasks parser ON parser.tenant_id=parse_ref.tenant_id AND parser.id=parse_ref.created_by_task_id
 JOIN photo.runs parse_run ON parse_run.tenant_id=parser.tenant_id AND parse_run.id=parser.run_id
 LEFT JOIN photo.v2_workflows parse_workflow ON parse_workflow.tenant_id=parser.tenant_id AND parse_workflow.run_id=parse_run.id
 JOIN photo.task_results parse_result ON parse_result.tenant_id=parser.tenant_id AND parse_result.id=parser.canonical_result_id AND parse_result.task_id=parser.id AND parse_result.attempt=parser.attempt AND parse_result.fencing_token=parser.fencing_token
 JOIN photo.task_attempts parse_attempt ON parse_attempt.tenant_id=parser.tenant_id AND parse_attempt.task_id=parser.id AND parse_attempt.attempt=parser.attempt AND parse_attempt.fencing_token=parser.fencing_token AND parse_attempt.finished_at IS NOT NULL
 JOIN photo.artifacts parse_pixels ON parse_pixels.tenant_id=parser.tenant_id AND parse_pixels.created_by_task_id=parser.id AND parse_pixels.output_role='face_labels'
 JOIN photo.artifacts parse_params ON parse_params.tenant_id=parser.tenant_id AND parse_params.id=parser.params_artifact_id
 WHERE parse_ref.tenant_id=p_tenant AND parse_ref.output_role='face_labels_ref'
 AND parser.operation='face.parse' AND parser.operation_version='1' AND parser.state='succeeded'
 AND parse_attempt.backend IN('cpu','cuda','mps') AND parser.execution_profile->>'backend'=parse_attempt.backend AND parse_attempt.profile->>'backend'=parse_attempt.backend
 AND (parse_workflow.project_id=p_project OR parse_run.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(parser.inputs)='array' AND parser.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
 AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',parse_ref.id,'sha256',parse_ref.sha256),jsonb_build_object('artifact_id',parse_pixels.id,'sha256',parse_pixels.sha256))
 AND jsonb_typeof(parse_result.outputs)='array' AND parse_result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',parse_ref.id,'sha256',parse_ref.sha256),jsonb_build_object('artifact_id',parse_pixels.id,'sha256',parse_pixels.sha256))
 AND parse_ref.media_type='application/json' AND parse_params.media_type='application/json' AND parse_pixels.media_type='application/octet-stream'
 AND parse_ref.bytes BETWEEN 2 AND 1048576 AND parse_params.bytes BETWEEN 2 AND 1048576 AND parse_pixels.bytes BETWEEN 32 AND 400065548
 AND parse_ref.produced_attempt=parser.attempt AND parse_ref.produced_fence=parser.fencing_token AND parse_pixels.produced_attempt=parser.attempt AND parse_pixels.produced_fence=parser.fencing_token
 AND NOT EXISTS(SELECT 1 FROM (VALUES(parse_ref.state,parse_ref.verified_at,parse_ref.object_version,parse_ref.expires_at),(parse_pixels.state,parse_pixels.verified_at,parse_pixels.object_version,parse_pixels.expires_at),(parse_params.state,parse_params.verified_at,parse_params.object_version,parse_params.expires_at)) parse_objects(state,verified,version,expires)
 WHERE state<>'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
 )))
 AND photo.semantic_defect_declares_input(p_tenant,defect.id,p_mask,p_mask_sha)
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pixel.id,'sha256',pixel.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 AND root.media_type='application/json' AND params.media_type='application/json' AND pixel.media_type='application/octet-stream'
 AND root.bytes BETWEEN 2 AND 1048576 AND params.bytes BETWEEN 2 AND 1048576 AND pixel.bytes BETWEEN 32 AND 800065548
 AND NOT EXISTS(SELECT 1 FROM (VALUES(root.state,root.verified_at,root.object_version,root.expires_at),(params.state,params.verified_at,params.object_version,params.expires_at),(pixel.state,pixel.verified_at,pixel.object_version,pixel.expires_at)) objects(state,verified,version,expires)
 WHERE state<>'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
 AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
 AND pixel.produced_attempt=producer.attempt AND pixel.produced_fence=producer.fencing_token
 ORDER BY root.id LIMIT 2;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_face_mask_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_face_mask_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_face_mask_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) TO photo_api;

CREATE OR REPLACE FUNCTION photo.semantic_proposal_defect_declared_protection_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_required uuid,p_required_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT required.bucket,required.object_key,required.object_version,required.bytes,required.media_type
 FROM photo.artifacts report
 JOIN photo.tasks producer ON producer.tenant_id=report.tenant_id AND producer.id=report.created_by_task_id
 JOIN photo.artifacts required ON required.tenant_id=producer.tenant_id AND required.id=p_required AND required.sha256=p_required_sha
 WHERE photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha)
 AND report.tenant_id=p_tenant AND report.id=p_report AND report.sha256=p_report_sha
 AND photo.semantic_defect_declares_input(p_tenant,producer.id,p_required,p_required_sha)
 AND required.state='ready' AND required.verified_at IS NOT NULL AND required.object_key<>''
 AND required.object_version IS NOT NULL AND required.object_version NOT IN('','null')
 AND (required.expires_at IS NULL OR required.expires_at>clock_timestamp())
 AND required.media_type='application/octet-stream' AND required.bytes BETWEEN 32 AND 800065548;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_declared_protection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_declared_protection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_declared_protection_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
