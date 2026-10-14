-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_label_png_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_png uuid,p_png_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT png.bucket,png.object_key,png.object_version,png.bytes,png.media_type
 FROM photo.artifacts report
 JOIN photo.tasks producer ON producer.tenant_id=report.tenant_id AND producer.id=report.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts png ON png.tenant_id=producer.tenant_id AND png.id=p_png AND png.sha256=p_png_sha AND png.created_by_task_id=producer.id
 WHERE photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha)
 AND report.tenant_id=p_tenant AND report.id=p_report AND report.sha256=p_report_sha
 AND png.output_role='defect_labels_png' AND png.state='ready' AND png.verified_at IS NOT NULL
 AND png.object_version IS NOT NULL AND png.object_version NOT IN('','null') AND png.object_key<>''
 AND (png.expires_at IS NULL OR png.expires_at>clock_timestamp())
 AND png.media_type='image/png' AND png.bytes BETWEEN 57 AND 613048576
 AND png.produced_attempt=producer.attempt AND png.produced_fence=producer.fencing_token
 AND jsonb_typeof(result.outputs)='array' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_png,'sha256',p_png_sha));
$$;
ALTER FUNCTION photo.semantic_proposal_defect_label_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_defect_label_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_defect_label_png_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
