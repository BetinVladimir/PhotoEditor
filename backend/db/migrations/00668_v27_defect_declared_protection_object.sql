-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_defect_declared_protection_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,p_report uuid,p_report_sha text,p_labels uuid,p_labels_sha text,p_protection uuid,p_protection_sha text,p_params uuid,p_params_sha text,p_required uuid,p_required_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT required.bucket,required.object_key,required.object_version,required.bytes,required.media_type
 FROM photo.artifacts report
 JOIN photo.tasks producer ON producer.tenant_id=report.tenant_id AND producer.id=report.created_by_task_id
 JOIN photo.artifacts required ON required.tenant_id=producer.tenant_id AND required.id=p_required AND required.sha256=p_required_sha
 WHERE photo.semantic_proposal_defect_origin(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_proposal,p_proposal_sha,p_report,p_report_sha,p_labels,p_labels_sha,p_protection,p_protection_sha,p_params,p_params_sha)
 AND report.tenant_id=p_tenant AND report.id=p_report AND report.sha256=p_report_sha
 AND jsonb_typeof(producer.inputs)='array' AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_required,'sha256',p_required_sha))
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
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
