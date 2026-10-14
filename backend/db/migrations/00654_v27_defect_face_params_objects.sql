-- +goose Up
-- Original canonical producer Params, not caller-selected documents.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_face_params_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_landmark uuid,p_landmark_sha text,p_detection uuid,p_detection_sha text)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT requested.contract,params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.bytes
 FROM (VALUES(0,'LandmarkParams'::text,p_landmark),(1,'FaceDetectParams'::text,p_detection)) requested(ordinal,contract,output_id)
 JOIN photo.artifacts output ON output.tenant_id=p_tenant AND output.id=requested.output_id
 JOIN photo.tasks producer ON producer.tenant_id=output.tenant_id AND producer.id=output.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 WHERE EXISTS(SELECT 1 FROM photo.semantic_proposal_defect_detection_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_landmark,p_landmark_sha,p_detection,p_detection_sha))
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
 AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp()) AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
 ORDER BY requested.ordinal;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_face_params_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_face_params_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_face_params_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
