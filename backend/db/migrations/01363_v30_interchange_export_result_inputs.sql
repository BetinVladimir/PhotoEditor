-- +goose Up
-- Result-phase input reads require a canonical succeeded export root and typed
-- membership in its sealed input inventory. No draft-scope fallback.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_result_input(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_root uuid,p_root_sha text,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;tid uuid;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_root IS NULL OR p_artifact IS NULL OR p_root=p_artifact OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_contract IS NULL OR p_contract NOT IN('Params','InterchangePlan','InputSnapshot','WorkingImage','FrozenRenderRecipe','EditingRecipeV3','StyleV2',
 'CorrectionPlan','Override','LayerStack','MaskRef','CandidateProfile','DescriptorShard','StyleBackgroundAsset','EncodedImage','WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray','DenseWarpField','DenseWarpArray') THEN RETURN;END IF;
 -- This helper rechecks current session/snapshot/project/download ACL and locks
 -- the exact run/task/canonical result plus verified root artifact.
 PERFORM 1 FROM photo.v30_interchange_export_output(p_tenant,p_actor,p_session,p_key,p_request_sha,p_root,p_root_sha,'InterchangeResult');
 IF NOT FOUND THEN RETURN;END IF;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);
 tid:=photo.v30_uuid5(photo.v30_uuid5(rid,'workflow'),'v2.interchange.export:0');
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=tid AND i.artifact_id=p_artifact AND i.sha256=p_sha AND i.contract=p_contract
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bytes BETWEEN 1 AND 137438953472 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND a.media_type=CASE WHEN p_contract='EncodedImage' THEN a.media_type
 WHEN p_contract IN('WorkingPixels','WorkingAlpha','PixelArray','MaskPixels','MaskArray','DenseWarpField','DenseWarpArray') THEN 'application/octet-stream' ELSE 'application/json' END
 AND (p_contract<>'EncodedImage' OR a.media_type IN('image/jpeg','image/png','image/tiff'))
 FOR SHARE OF i,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
