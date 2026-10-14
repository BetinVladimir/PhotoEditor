-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_skin_png_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_mask uuid,p_mask_sha text,p_selector text)
RETURNS TABLE(png_id uuid,png_sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT png.id,png.sha256,png.bucket,png.object_key,png.object_version,png.bytes
 FROM photo.artifacts pixel
 JOIN photo.tasks producer ON producer.tenant_id=pixel.tenant_id AND producer.id=pixel.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts png ON png.tenant_id=producer.tenant_id AND png.created_by_task_id=producer.id
 WHERE p_selector='skin' AND (SELECT count(*) FROM photo.semantic_proposal_defect_face_mask_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha,p_mask,p_mask_sha,p_selector))=1
 AND pixel.tenant_id=p_tenant AND pixel.id=p_mask AND pixel.sha256=p_mask_sha
 AND png.output_role='skin_mask_png' AND png.state='ready' AND png.verified_at IS NOT NULL
 AND png.object_version IS NOT NULL AND png.object_version NOT IN('','null') AND png.object_key<>''
 AND (png.expires_at IS NULL OR png.expires_at>clock_timestamp())
 AND png.media_type='image/png' AND png.bytes BETWEEN 57 AND 613048576
 AND png.produced_attempt=producer.attempt AND png.produced_fence=producer.fencing_token
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',png.id,'sha256',png.sha256))
 ORDER BY png.id LIMIT 2;
$$;
ALTER FUNCTION photo.semantic_proposal_defect_skin_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_skin_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_skin_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
