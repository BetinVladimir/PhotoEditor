-- +goose Up
-- Receipt-backed accounting only; an arbitrary artifact row is not charge proof.
ALTER TABLE photo.artifacts ADD COLUMN gc_storage_settled boolean NOT NULL DEFAULT false;
-- +goose StatementBegin
-- Existing API table-level UPDATE grants include future columns. Protect the
-- accounting marker explicitly; column REVOKE cannot override table privileges.
CREATE FUNCTION photo.guard_artifact_gc_settlement() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.gc_storage_settled AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'ARTIFACT_GC_SETTLEMENT_PRIVATE' USING ERRCODE='42501';END IF;
 ELSIF NEW.gc_storage_settled IS DISTINCT FROM OLD.gc_storage_settled AND current_user<>'photo_fn_owner' THEN
  RAISE EXCEPTION 'ARTIFACT_GC_SETTLEMENT_PRIVATE' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_artifact_gc_settlement() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_artifact_gc_settlement()
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
CREATE TRIGGER artifact_gc_settlement_private BEFORE INSERT OR UPDATE OF gc_storage_settled ON photo.artifacts
 FOR EACH ROW EXECUTE FUNCTION photo.guard_artifact_gc_settlement();
CREATE FUNCTION photo.artifact_gc_charged_bytes(p_tenant uuid,p_artifact uuid) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE a photo.artifacts%ROWTYPE; receipts integer; known_receipts integer;
BEGIN
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact;
 IF a.id IS NULL THEN RAISE EXCEPTION 'ARTIFACT_ACCOUNTING_IDENTITY_MISSING';END IF;
 -- Dedicated ingest cleanup owns these storage debits; generic GC must not guess zero.
 IF EXISTS(SELECT FROM photo.ingest_originals x WHERE x.tenant_id=a.tenant_id AND x.artifact_id=a.id)
 OR EXISTS(SELECT FROM photo.staging_cleanup x WHERE x.tenant_id=a.tenant_id AND x.artifact_id=a.id)
 OR EXISTS(SELECT FROM photo.ingest_upload_bindings x WHERE x.tenant_id=a.tenant_id AND x.source_id=a.id)
 OR a.object_key LIKE 'originals/tenants/%' OR a.output_role='original' THEN
  RAISE EXCEPTION 'ARTIFACT_ACCOUNTING_DEDICATED_INGEST' USING ERRCODE='23514';END IF;

 SELECT (SELECT count(*) FROM photo.output_uploads u WHERE u.tenant_id=a.tenant_id AND u.artifact_id=a.id)
  +(SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id)
  +(SELECT count(*) FROM photo.v2_draft_uploads d WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id)
  +(SELECT count(*) FROM photo.tenant_static_resources r WHERE r.tenant_id=a.tenant_id AND r.artifact_id=a.id)
  +(SELECT count(*) FROM photo.v30_archive_uploads u WHERE u.tenant_id=a.tenant_id AND u.artifact_id=a.id)
  +(SELECT count(*) FROM photo.v30_export_snapshot_documents d WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id)
 INTO known_receipts;
 SELECT count(*) INTO receipts FROM (
  SELECT u.id FROM photo.output_uploads u WHERE u.tenant_id=a.tenant_id AND u.artifact_id=a.id AND u.state='ready'
   AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version
   AND u.media_type=a.media_type AND u.role=a.output_role
  UNION ALL
  SELECT d.id FROM photo.backend_documents d WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.state='ready'
   AND d.bytes=a.bytes AND d.sha256=a.sha256 AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.object_key='tenants/'||a.tenant_id||'/runs/'||d.run_id||'/documents/'||a.id
   AND a.output_role=d.purpose
   AND ((d.purpose<>'v30_import_prepared' AND a.media_type='application/json')
    OR (d.purpose='v30_import_prepared' AND a.media_type=d.v30_import_prepared->>'media_type'
     AND ((d.v30_import_prepared->>'contract' IN('MaskPixels','DenseWarpField','AbsoluteLUTArray','DefectLabelMap','WorkingPixels','WorkingAlpha') AND a.media_type='application/octet-stream')
      OR (d.v30_import_prepared->>'contract' IN('EditingRecipeV3','StyleV2','CorrectionPlan','Override','LayerStack','MaskRef','Params','Stats20','ResolvedStyle','DefectCandidates','DefectReview','DustCandidates','DefectProposeReport','WorkingImage','FaceLandmarks','FaceBoxes','HistoricalOriginReceipt') AND a.media_type='application/json')
      OR (d.v30_import_prepared->>'contract'='DefectLabelPNG' AND a.media_type='image/png')
      OR (d.v30_import_prepared->>'contract'='StyleBackgroundAsset' AND a.media_type IN('image/jpeg','image/png','image/tiff')))))
  UNION ALL
  SELECT d.document_id FROM photo.v2_draft_uploads d WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.state IN('ready','retired')
   AND d.bytes=a.bytes AND d.sha256=a.sha256 AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.object_key='tenants/'||a.tenant_id||'/v2-drafts/'||a.id
   AND ((d.upload_contract='Document' AND a.media_type='application/json' AND a.output_role='document')
    OR (d.upload_contract='MaskPixels' AND a.media_type='application/x-npy' AND a.output_role='mask_pixels'))
  UNION ALL
  SELECT r.artifact_id FROM photo.tenant_static_resources r WHERE r.tenant_id=a.tenant_id AND r.artifact_id=a.id AND r.state='ready'
   AND r.bytes=a.bytes AND r.sha256=a.sha256 AND r.bucket=a.bucket AND r.object_version=a.object_version
   AND a.object_key='tenants/'||a.tenant_id||'/resources/'||a.id
   AND a.media_type='font/ttf' AND a.output_role='system_resource'
  UNION ALL
  SELECT u.id FROM photo.v30_archive_uploads u WHERE u.tenant_id=a.tenant_id AND u.artifact_id=a.id AND u.state='verified'
   AND u.expected_bytes=a.bytes AND u.expected_sha=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key
   AND u.object_version=a.object_version AND a.output_role='v30_interchange_archive' AND a.media_type='application/zip'
  UNION ALL
  SELECT d.artifact_id FROM photo.v30_export_snapshot_documents d
   WHERE d.tenant_id=a.tenant_id AND d.artifact_id=a.id AND d.state='ready'
   AND d.bytes=a.bytes AND d.sha256=a.sha256 AND d.bucket=a.bucket AND d.object_version=a.object_version
   AND a.object_key='tenants/'||a.tenant_id||'/runs/'||d.snapshot_id||'/documents/'||a.id
   AND a.media_type='application/json' AND a.output_role='v30_export_snapshot'
 ) charged;
 IF known_receipts<>receipts THEN RAISE EXCEPTION 'ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH' USING ERRCODE='23514';END IF;
 IF receipts>1 THEN RAISE EXCEPTION 'ARTIFACT_ACCOUNTING_PROVENANCE_AMBIGUOUS' USING ERRCODE='23514';END IF;
 IF receipts=1 THEN RETURN a.bytes;END IF;
 RAISE EXCEPTION 'ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN' USING ERRCODE='23514';
END $$;
ALTER FUNCTION photo.artifact_gc_charged_bytes(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.artifact_gc_charged_bytes(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
CREATE OR REPLACE FUNCTION photo.finish_artifact_gc(p_artifact uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.artifacts%ROWTYPE;a photo.artifacts%ROWTYPE;charged bigint;
BEGIN
 SELECT * INTO hint FROM photo.artifacts WHERE id=p_artifact;
 IF hint.id IS NULL OR p_token IS NULL THEN RETURN false;END IF;
 -- Writers acquire usage before artifact publication. Keep the same order here.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE id=p_artifact AND tenant_id=hint.tenant_id FOR UPDATE;
 IF a.id IS NULL OR a.gc_token IS DISTINCT FROM p_token THEN RETURN false;END IF;
 IF a.state='deleted' THEN RETURN a.gc_storage_settled;END IF;
 IF a.state<>'deleting' OR a.gc_lease_until IS NULL OR a.gc_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF photo.artifact_has_references(a.tenant_id,a.id) THEN
  RAISE EXCEPTION 'ARTIFACT_GC_REFERENCE_RACE' USING ERRCODE='40001';END IF;
 -- 02539: receipt-backed storage settlement before deleted publication.
 charged:=photo.artifact_gc_charged_bytes(a.tenant_id,a.id);
 -- No settlement if physical-cleanup authority expired during a reference check.
 IF a.gc_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF NOT a.gc_storage_settled AND charged>0 THEN
  UPDATE photo.tenant_usage SET storage_bytes=storage_bytes-charged
   WHERE tenant_id=a.tenant_id AND storage_bytes>=charged;
  IF NOT FOUND THEN RAISE EXCEPTION 'ARTIFACT_GC_STORAGE_ACCOUNTING' USING ERRCODE='23514';END IF;
 END IF;
 UPDATE photo.artifacts SET state='deleted',gc_lease_until=NULL,verified_at=NULL,gc_storage_settled=true WHERE id=a.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.finish_artifact_gc(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_artifact_gc(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.finish_artifact_gc(uuid,uuid) TO photo_maintenance;
-- Exclude dedicated ingest storage before choosing a generic candidate, so the
-- cleaner does not purge an object whose settlement belongs to another queue.
DO $$
DECLARE source text;definition text;
 needle constant text:=' ORDER BY x.expires_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;';
BEGIN
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid='photo.claim_artifact_gc()'::regprocedure AND prosecdef
 AND pg_get_userbyid(proowner)='photo_fn_owner';
 IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'ARTIFACT_GC_DEDICATED_CLAIM_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,source,replace(source,needle,$filter$
 -- 02539: generic GC must leave dedicated ingest storage to its cleanup queue.
 AND NOT EXISTS(SELECT FROM photo.ingest_originals d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.staging_cleanup d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
 AND NOT EXISTS(SELECT FROM photo.ingest_upload_bindings d WHERE d.tenant_id=x.tenant_id AND d.source_id=x.id)
 AND x.object_key NOT LIKE 'originals/tenants/%' AND x.output_role IS DISTINCT FROM 'original'
 AND (EXISTS(SELECT FROM photo.output_uploads d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
  OR EXISTS(SELECT FROM photo.backend_documents d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
  OR EXISTS(SELECT FROM photo.v2_draft_uploads d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
  OR EXISTS(SELECT FROM photo.tenant_static_resources d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
  OR EXISTS(SELECT FROM photo.v30_archive_uploads d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id)
  OR EXISTS(SELECT FROM photo.v30_export_snapshot_documents d WHERE d.tenant_id=x.tenant_id AND d.artifact_id=x.id))
$filter$||needle));
 -- Verify exact receipt before detaching references or permitting physical purge.
 SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
 WHERE oid='photo.claim_artifact_gc()'::regprocedure;
 IF (length(source)-length(replace(source,' IF NOT FOUND THEN RETURN;END IF;','')))/length(' IF NOT FOUND THEN RETURN;END IF;')<>1 THEN
  RAISE EXCEPTION 'ARTIFACT_GC_CHARGE_PREFLIGHT_SOURCE_SHAPE';END IF;
 EXECUTE replace(definition,source,replace(source,' IF NOT FOUND THEN RETURN;END IF;',
  ' IF NOT FOUND THEN RETURN;END IF;'||chr(10)||' PERFORM photo.artifact_gc_charged_bytes(a.tenant_id,a.id);'));
END $$;
-- Dedicated original/staging cleanup already debits charged storage. Record
-- that exact settlement in the same transaction; do not debit through generic GC.
DO $$
DECLARE signature text;source text;definition text;late_guard text;patched text;
 debit constant text:=' UPDATE photo.tenant_usage SET storage_bytes=storage_bytes-';
 needle constant text:='UPDATE photo.artifacts SET state=''deleted'' WHERE';
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.finish_original_cleanup(uuid,uuid)','photo.finish_staging_cleanup(uuid,uuid)'] LOOP
  SELECT prosrc,pg_get_functiondef(oid) INTO source,definition FROM pg_proc
   WHERE oid=signature::regprocedure AND prosecdef AND pg_get_userbyid(proowner)='photo_fn_owner';
  IF source IS NULL OR (length(source)-length(replace(source,needle,'')))/length(needle)<>1
   OR strpos(source,'storage_bytes=storage_bytes-')=0 THEN
   RAISE EXCEPTION 'DEDICATED_STORAGE_SETTLEMENT_SOURCE_SHAPE';END IF;
  IF (length(source)-length(replace(source,debit,'')))/length(debit)<>1 THEN
   RAISE EXCEPTION 'DEDICATED_STORAGE_DEBIT_SOURCE_SHAPE';END IF;
  IF signature='photo.finish_original_cleanup(uuid,uuid)' THEN
   late_guard:=' -- 02539: repeat cleanup clock after artifact row-lock wait.'||chr(10)||
    ' IF d.cleanup_state<>''cleaning'' OR d.cleanup_token IS DISTINCT FROM p_token'||chr(10)||
    ' OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp() THEN RETURN false;END IF;'||chr(10);
  ELSE
   late_guard:=' -- 02539: repeat cleanup clock after artifact row-lock wait.'||chr(10)||
    ' IF d.state<>''cleaning'' OR d.token IS DISTINCT FROM p_token'||chr(10)||
    ' OR d.lease_until IS NULL OR d.lease_until<=clock_timestamp() THEN RETURN false;END IF;'||chr(10);
  END IF;
  patched:=replace(source,needle,'UPDATE photo.artifacts SET state=''deleted'',gc_storage_settled=true WHERE');
  patched:=replace(patched,debit,late_guard||debit);
  EXECUTE replace(definition,source,patched);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
