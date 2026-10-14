-- +goose Up
-- External vectors must be declared by the same canonical manual pixel producer.
-- Original Params/root/source-bound ManualMask content is verified by Go.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_manual_vector_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_mask uuid,p_mask_sha text,p_selector text,p_vectors uuid,p_vectors_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT vectors.bucket,vectors.object_key,vectors.object_version,vectors.bytes
 FROM photo.artifacts vectors
 JOIN photo.artifacts pixel ON pixel.tenant_id=vectors.tenant_id AND pixel.id=p_mask AND pixel.sha256=p_mask_sha
 JOIN photo.tasks producer ON producer.tenant_id=pixel.tenant_id AND producer.id=pixel.created_by_task_id
 WHERE vectors.tenant_id=p_tenant AND vectors.id=p_vectors AND vectors.sha256=p_vectors_sha
 AND p_selector='manual'
 AND (SELECT count(*) FROM photo.semantic_proposal_defect_face_mask_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_mask,p_mask_sha,p_selector))=1
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_vectors,'sha256',p_vectors_sha))
 AND vectors.state='ready' AND vectors.verified_at IS NOT NULL AND vectors.media_type='application/json'
 AND vectors.bytes BETWEEN 2 AND 1048576 AND vectors.object_version IS NOT NULL AND vectors.object_version NOT IN('','null')
 AND (vectors.expires_at IS NULL OR vectors.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_proposal_defect_manual_vector_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_manual_vector_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_manual_vector_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
