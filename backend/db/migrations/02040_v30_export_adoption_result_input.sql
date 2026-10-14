-- +goose Up
-- Typed sealed input lookup for physical export closure under a scheduler lease.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_result_input(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_contract IS NULL OR p_contract NOT IN('Params','InterchangePlan','InputSnapshot',
 'WorkingImage','FrozenRenderRecipe','EditingRecipeV3','StyleV2','CorrectionPlan','Override',
 'LayerStack','MaskRef','CandidateProfile','DescriptorShard','StyleBackgroundAsset',
 'EncodedImage','WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray',
 'DenseWarpField','DenseWarpArray') THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
  AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=p_artifact
  AND i.sha256=p_sha AND i.contract=p_contract
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 1 AND 137438953472
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.media_type=CASE WHEN p_contract='EncodedImage' THEN a.media_type
   WHEN p_contract IN('WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray',
    'DenseWarpField','DenseWarpArray') THEN 'application/octet-stream'
   ELSE 'application/json' END
  AND (p_contract<>'EncodedImage' OR a.media_type IN('image/jpeg','image/png','image/tiff'))
 FOR SHARE OF i,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
