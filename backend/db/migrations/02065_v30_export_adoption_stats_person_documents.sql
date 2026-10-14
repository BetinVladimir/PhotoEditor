-- +goose Up
-- Reconstruct a current claimed Stats20→mask.person lineage before returning
-- one original producer document. PersonParams/Report are not generic Params.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_stats_person_document(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_root uuid,p_root_sha text,p_original uuid,p_original_sha text,
 p_report uuid,p_report_sha text,p_mask_sha text,p_mask_key text,
 p_mask_version text,p_mask_bytes bigint,p_mask_type text,p_bucket text,
 p_selected uuid,p_selected_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_root IS NULL OR p_original IS NULL OR p_report IS NULL OR p_selected IS NULL
  OR p_root=p_original OR p_root=p_report OR p_original=p_report
  OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_original_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_selected_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_key,'')='' OR coalesce(p_mask_version,'') IN ('','null')
  OR p_mask_bytes<1 OR p_mask_type NOT IN('application/octet-stream','application/x-npy')
  OR coalesce(p_bucket,'')='' OR p_contract NOT IN('MaskRef','PersonParams','PersonReport')
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT selected.bucket,selected.object_key,selected.object_version,
  selected.sha256,selected.bytes,selected.media_type
 FROM photo.v2_task_inputs sealed
 JOIN photo.artifacts stats ON stats.tenant_id=sealed.tenant_id
  AND stats.id=sealed.artifact_id AND stats.sha256=sealed.sha256
 JOIN photo.tasks stat_task ON stat_task.tenant_id=stats.tenant_id
  AND stat_task.id=stats.created_by_task_id
 JOIN photo.artifacts stat_params ON stat_params.tenant_id=stat_task.tenant_id
  AND stat_params.id=stat_task.params_artifact_id
 JOIN photo.snapshot_items item ON item.tenant_id=sealed.tenant_id
  AND item.snapshot_id=w.input_snapshot_id
 JOIN photo.artifacts mask ON mask.tenant_id=sealed.tenant_id
  AND mask.sha256=p_mask_sha AND mask.bucket=p_bucket
  AND mask.object_key=p_mask_key AND mask.object_version=p_mask_version
  AND mask.bytes=p_mask_bytes AND mask.media_type=p_mask_type
  AND stat_task.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),
   jsonb_build_object('artifact_id',item.artifact_id,'sha256',item.sha256))
 JOIN LATERAL photo.v30_export_adoption_stats_person_origin(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  stats.id,stats.sha256,stat_params.id,stat_params.sha256,
  mask.id,mask.sha256,item.artifact_id,item.sha256,p_bucket) current_origin
  ON current_origin.root_id=p_root AND current_origin.root_sha=p_root_sha
  AND current_origin.original_id=p_original AND current_origin.original_sha=p_original_sha
  AND current_origin.report_id=p_report AND current_origin.report_sha=p_report_sha
  AND current_origin.mask_sha256=p_mask_sha
  AND current_origin.mask_key=p_mask_key
  AND current_origin.mask_version=p_mask_version
  AND current_origin.mask_bytes=p_mask_bytes
  AND current_origin.mask_type=p_mask_type
 JOIN photo.artifacts selected ON selected.tenant_id=sealed.tenant_id
  AND selected.id=p_selected AND selected.sha256=p_selected_sha
 WHERE sealed.tenant_id=p_tenant AND sealed.task_id=p_task AND sealed.contract='Stats20'
  AND ((p_contract='MaskRef' AND selected.id=p_root AND selected.sha256=p_root_sha)
   OR (p_contract='PersonParams' AND selected.id=p_original AND selected.sha256=p_original_sha)
   OR (p_contract='PersonReport' AND selected.id=p_report AND selected.sha256=p_report_sha))
  AND selected.bucket=p_bucket AND selected.media_type='application/json'
  AND selected.bytes BETWEEN 2 AND CASE p_contract
   WHEN 'PersonParams' THEN 65536 ELSE 1048576 END
  AND selected.state='ready' AND selected.verified_at IS NOT NULL
  AND selected.object_version IS NOT NULL AND selected.object_version NOT IN('','null')
  AND (selected.expires_at IS NULL OR selected.expires_at>clock_timestamp())
 LIMIT 2;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_stats_person_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,text,bigint,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_stats_person_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,text,bigint,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_stats_person_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,text,bigint,text,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
