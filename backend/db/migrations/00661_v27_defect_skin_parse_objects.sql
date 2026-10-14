-- +goose Up
-- Original parse manifest/Params/labels for an exact declared skin dependency.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_skin_parse_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_mask uuid,p_mask_sha text,p_selector text,p_parse uuid,p_parse_sha text)
RETURNS TABLE(contract text,id uuid,sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT selected.contract,object.id,object.sha256,object.bucket,object.object_key,object.object_version,object.bytes
 FROM photo.artifacts parse_ref
 JOIN photo.tasks parser ON parser.tenant_id=parse_ref.tenant_id AND parser.id=parse_ref.created_by_task_id
 JOIN photo.runs parse_run ON parse_run.tenant_id=parser.tenant_id AND parse_run.id=parser.run_id
 LEFT JOIN photo.v2_workflows parse_workflow ON parse_workflow.tenant_id=parser.tenant_id AND parse_workflow.run_id=parse_run.id
 JOIN photo.task_results parse_result ON parse_result.tenant_id=parser.tenant_id AND parse_result.id=parser.canonical_result_id AND parse_result.task_id=parser.id AND parse_result.attempt=parser.attempt AND parse_result.fencing_token=parser.fencing_token
 JOIN photo.task_attempts parse_attempt ON parse_attempt.tenant_id=parser.tenant_id AND parse_attempt.task_id=parser.id AND parse_attempt.attempt=parser.attempt AND parse_attempt.fencing_token=parser.fencing_token AND parse_attempt.finished_at IS NOT NULL
 JOIN photo.artifacts parse_pixels ON parse_pixels.tenant_id=parser.tenant_id AND parse_pixels.created_by_task_id=parser.id AND parse_pixels.output_role='face_labels'
 JOIN photo.artifacts parse_params ON parse_params.tenant_id=parser.tenant_id AND parse_params.id=parser.params_artifact_id
 JOIN photo.artifacts mask_pixel ON mask_pixel.tenant_id=parser.tenant_id AND mask_pixel.id=p_mask AND mask_pixel.sha256=p_mask_sha
 JOIN photo.tasks producer ON producer.tenant_id=mask_pixel.tenant_id AND producer.id=mask_pixel.created_by_task_id
 CROSS JOIN LATERAL (VALUES('manifest',parse_ref.id),('params',parse_params.id),('pixels',parse_pixels.id)) selected(contract,id)
 JOIN photo.artifacts object ON object.tenant_id=parser.tenant_id AND object.id=selected.id
 WHERE parse_ref.id=p_parse AND parse_ref.sha256=p_parse_sha AND p_selector='skin'
 AND (SELECT count(*) FROM photo.semantic_proposal_defect_face_mask_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_mask,p_mask_sha,p_selector))=1
 AND parse_ref.tenant_id=p_tenant AND parse_ref.output_role='face_labels_ref'
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
 ORDER BY selected.contract,object.id LIMIT 6;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_skin_parse_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_skin_parse_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_skin_parse_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
