-- +goose Up
-- Current claim-scoped native defects.propose report/PNG/protection occurrence.
-- Physical PNG/NPY equality and typed report schema remain mandatory in Go.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_defect_png(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_source uuid,p_source_sha text,p_proposal uuid,p_proposal_sha text,
 p_report uuid,p_report_sha text,p_protection uuid,p_protection_sha text,
 p_png uuid,p_png_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE labels uuid;labels_sha text;label_count integer;
BEGIN
 IF p_report IS NULL OR p_protection IS NULL OR p_png IS NULL
 OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_protection_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_png_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
 p_actor,p_key,p_request_sha,p_token,p_report,p_report_sha,'DefectProposeReport');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
 p_actor,p_key,p_request_sha,p_token,p_png,p_png_sha,'DefectLabelPNG');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT count(*),min(label.id::text)::uuid,min(label.sha256) INTO label_count,labels,labels_sha FROM photo.artifacts proposal
 JOIN photo.artifacts label ON label.tenant_id=proposal.tenant_id
 AND label.created_by_task_id=proposal.created_by_task_id AND label.output_role='defect_labels'
 WHERE proposal.tenant_id=p_tenant AND proposal.id=p_proposal AND proposal.sha256=p_proposal_sha;
 IF label_count<>1 OR labels IS NULL OR NOT photo.v30_export_adoption_defect_label(p_tenant,p_workflow,p_run,p_task,p_result,
 p_actor,p_key,p_request_sha,p_token,p_source,p_source_sha,p_proposal,p_proposal_sha,labels,labels_sha) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts proposal
 JOIN photo.tasks producer ON producer.tenant_id=proposal.tenant_id AND producer.id=proposal.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id
 AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id
 AND report.id=p_report AND report.sha256=p_report_sha AND report.output_role='defect_propose_report'
 JOIN photo.artifacts protection ON protection.tenant_id=producer.tenant_id AND protection.created_by_task_id=producer.id
 AND protection.id=p_protection AND protection.sha256=p_protection_sha AND protection.output_role='defect_protection'
 JOIN photo.artifacts png ON png.tenant_id=producer.tenant_id AND png.created_by_task_id=producer.id
 AND png.id=p_png AND png.sha256=p_png_sha AND png.output_role='defect_labels_png'
 WHERE proposal.tenant_id=p_tenant AND proposal.id=p_proposal AND proposal.sha256=p_proposal_sha
 AND producer.state='succeeded' AND NOT producer.cancel_requested
 AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
 AND protection.produced_attempt=producer.attempt AND protection.produced_fence=producer.fencing_token
 AND png.produced_attempt=producer.attempt AND png.produced_fence=producer.fencing_token
 AND png.media_type='image/png' AND png.bytes BETWEEN 57 AND 536870912
 AND result.outputs @> jsonb_build_array(
 jsonb_build_object('artifact_id',p_report,'sha256',p_report_sha),
 jsonb_build_object('artifact_id',p_protection,'sha256',p_protection_sha),
 jsonb_build_object('artifact_id',p_png,'sha256',p_png_sha))
 AND NOT EXISTS(SELECT 1 FROM (VALUES
 (report.state,report.verified_at,report.object_version,report.expires_at),
 (protection.state,protection.verified_at,protection.object_version,protection.expires_at),
 (png.state,png.verified_at,png.object_version,png.expires_at)) checked(state,verified,version,expires)
 WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_defect_png(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_defect_png(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_defect_png(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
