-- +goose Up
-- Forward-only: a person-mask grant cannot authorize recipe installation or
-- worker use without an independently recorded, still-live producer receipt.
-- The worker entry point calls this public function by name and inherits the
-- same gate without changing the already leased worker contract.
-- +goose StatementBegin
ALTER FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 RENAME TO v30_import_person_mask_prepared_live_pre_origin;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_prepared_live_pre_origin(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_api,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_import_person_mask_prepared_live(
 p_tenant uuid,p_workflow uuid,p_target_version uuid,p_prepared_mask uuid,p_mask_sha text,p_parsed text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE epoch bigint;matches integer;
BEGIN
 epoch:=photo.v30_import_person_mask_prepared_live_pre_origin(
  p_tenant,p_workflow,p_target_version,p_prepared_mask,p_mask_sha,p_parsed);
 IF epoch IS NULL OR epoch<1 THEN RETURN NULL;END IF;
 SELECT count(*) INTO matches FROM photo.v30_import_person_mask_confirmations c
 JOIN photo.v30_import_person_origin_receipts r
  ON r.tenant_id=c.tenant_id AND r.workflow_id=c.workflow_id
  AND r.project_id=c.project_id AND r.entry_id=c.entry_id
  AND r.source_version_id=c.source_version_id AND r.target_version_id=c.target_version_id
  AND r.recipe_id=c.recipe_id AND r.recipe_sha=c.recipe_sha
  AND r.override_id=c.override_id AND r.override_sha=c.override_sha
  AND r.mask_pixels_id=c.mask_id AND r.mask_pixels_sha=c.mask_sha
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow AND c.target_version_id=p_target_version
  AND c.mask_sha=p_mask_sha AND c.status='granted' AND c.epoch=epoch
  AND photo.v30_uuid5(p_workflow,'interchange:dependency:'||c.entry_id::text||':MaskPixels:'||c.mask_id::text)=p_prepared_mask
  AND photo.v30_import_person_origin_receipt_live(p_tenant,p_workflow,c.entry_id,p_target_version,
   r.recipe_id,r.recipe_sha,r.override_id,r.override_sha,r.mask_ref_id,r.mask_ref_sha,
   r.mask_pixels_id,r.mask_pixels_sha,r.receipt_artifact_id,r.receipt_sha,p_parsed);
 IF matches<>1 THEN RETURN NULL;END IF;
 RETURN epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
