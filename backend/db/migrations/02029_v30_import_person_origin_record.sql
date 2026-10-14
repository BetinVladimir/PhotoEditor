-- +goose Up
-- Only the scheduler process may call this after independently verifying the
-- Ed25519 signature and exact versioned receipt object bytes. SQL repeats the
-- current import/manifest/file scope; it never treats consent as provenance.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v30_import_person_origin(
 p_tenant uuid,p_workflow uuid,p_target uuid,p_parsed text,
 p_receipt_artifact uuid,p_receipt_raw text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;body jsonb;claim jsonb;mapping jsonb;file jsonb;item jsonb;
 w photo.v2_workflows%ROWTYPE;existing photo.v30_import_person_origin_receipts%ROWTYPE;
 receipt_sha text;entry_id uuid;recipe_id uuid;override_id uuid;mask_ref_id uuid;pixels_id uuid;
 recipe_sha text;override_sha text;mask_ref_sha text;pixels_sha text;
 source_version uuid;issuer uuid;key_id text;task uuid;attempt integer;fence bigint;
 match_count integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_workflow IS NULL OR p_target IS NULL OR p_receipt_artifact IS NULL
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR p_receipt_raw IS NULL OR octet_length(p_receipt_raw) NOT BETWEEN 2 AND 16384 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant AND x.id=p_workflow
  AND x.workflow_type='interchange' AND x.renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN false;END IF;
 body:=p_receipt_raw::jsonb;claim:=body->'claim';
 IF body->>'schema_version' IS DISTINCT FROM '1' OR claim->>'schema_version' IS DISTINCT FROM '1'
 OR claim->>'operation' IS DISTINCT FROM 'mask.person' OR claim->>'operation_version' IS DISTINCT FROM '1'
 OR claim->>'entry_id' IS NULL OR claim->>'issuer_id' IS NULL
 OR body->>'signature_hex' IS NULL OR length(body->>'signature_hex')<>128 THEN RETURN false;END IF;
 receipt_sha:=encode(sha256(convert_to(p_receipt_raw,'UTF8')),'hex');
 entry_id:=(claim->>'entry_id')::uuid;source_version:=(claim#>>'{source,asset_version_id}')::uuid;
 recipe_id:=(claim#>>'{recipe,artifact_id}')::uuid;recipe_sha:=claim#>>'{recipe,sha256}';
 override_id:=(claim#>>'{override,artifact_id}')::uuid;override_sha:=claim#>>'{override,sha256}';
 mask_ref_id:=(claim#>>'{mask_ref,artifact_id}')::uuid;mask_ref_sha:=claim#>>'{mask_ref,sha256}';
 pixels_id:=(claim#>>'{mask_pixels,artifact_id}')::uuid;pixels_sha:=claim#>>'{mask_pixels,sha256}';
 issuer:=(claim->>'issuer_id')::uuid;key_id:=body->>'key_id';
 task:=(claim->>'producer_task_id')::uuid;attempt:=(claim->>'producer_attempt')::integer;
 fence:=(claim->>'producer_fence')::bigint;
 IF entry_id IS NULL OR source_version IS NULL OR recipe_id IS NULL OR override_id IS NULL
 OR mask_ref_id IS NULL OR pixels_id IS NULL OR issuer IS NULL OR task IS NULL
 OR attempt<1 OR fence<1 OR coalesce(key_id,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(recipe_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(override_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(mask_ref_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(pixels_sha,'') !~ '^[0-9a-f]{64}$'
 OR claim#>>'{source,source,artifact_id}' IS NULL
 OR coalesce(claim#>>'{source,source,sha256}','') !~ '^[0-9a-f]{64}$'
 OR coalesce(claim#>>'{source,canvas_hash}','') !~ '^[0-9a-f]{64}$'
 OR coalesce(claim->>'model_lock_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') e
  WHERE e->>'entry_id'=entry_id::text AND e->>'state'='adopted'
  AND e->>'asset_version_id'=p_target::text) THEN RETURN false;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,mapping
 FROM jsonb_array_elements(coalesce(p_parsed::jsonb->'manifest'->'person_origins','[]'::jsonb))
 WHERE value->>'entry_id'=entry_id::text
  AND value->'recipe'=claim->'recipe' AND value->'override'=claim->'override'
  AND value->'mask_ref'=claim->'mask_ref' AND value->'mask_pixels'=claim->'mask_pixels'
  AND value->>'sha256'=receipt_sha;
 IF match_count<>1 THEN RETURN false;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,item
 FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'inventory')
 WHERE value->>'path'=mapping->>'path' AND value->>'sha256'=receipt_sha
  AND value->>'contract'='PersonOriginReceipt';
 IF match_count<>1 THEN RETURN false;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,file
 FROM jsonb_array_elements(p_parsed::jsonb->'files')
 WHERE value->>'path'=mapping->>'path' AND value->>'contract'='PersonOriginReceipt'
  AND value#>>'{content,artifact_id}'=p_receipt_artifact::text
  AND value#>>'{content,sha256}'=receipt_sha;
 IF match_count<>1 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_receipt_artifact
  AND a.sha256=receipt_sha AND a.state='ready' AND a.output_role='imported_file'
  AND a.media_type='application/octet-stream' AND a.bytes=octet_length(p_receipt_raw)
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO existing FROM photo.v30_import_person_origin_receipts r
 WHERE r.tenant_id=p_tenant AND r.workflow_id=p_workflow AND r.entry_id=entry_id
  AND r.override_id=override_id AND r.mask_ref_id=mask_ref_id AND r.target_version_id=p_target FOR SHARE;
 IF FOUND THEN
  RETURN existing.project_id=w.project_id AND existing.source_version_id=source_version
   AND existing.recipe_id=recipe_id AND existing.recipe_sha=recipe_sha
   AND existing.override_sha=override_sha AND existing.mask_ref_sha=mask_ref_sha
   AND existing.mask_pixels_id=pixels_id AND existing.mask_pixels_sha=pixels_sha
   AND existing.receipt_artifact_id=p_receipt_artifact AND existing.receipt_sha=receipt_sha
   AND existing.issuer_id=issuer AND existing.key_id=key_id
   AND existing.producer_task_id=task AND existing.producer_attempt=attempt
   AND existing.producer_fence=fence;
 END IF;
 INSERT INTO photo.v30_import_person_origin_receipts(
  tenant_id,workflow_id,project_id,entry_id,source_version_id,target_version_id,
  recipe_id,recipe_sha,override_id,override_sha,mask_ref_id,mask_ref_sha,
  mask_pixels_id,mask_pixels_sha,receipt_artifact_id,receipt_sha,issuer_id,key_id,
  producer_task_id,producer_attempt,producer_fence)
 VALUES(p_tenant,p_workflow,w.project_id,entry_id,source_version,p_target,
  recipe_id,recipe_sha,override_id,override_sha,mask_ref_id,mask_ref_sha,
  pixels_id,pixels_sha,p_receipt_artifact,receipt_sha,issuer,key_id,task,attempt,fence);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value OR unique_violation OR check_violation THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v30_import_person_origin(uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v30_import_person_origin(uuid,uuid,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.record_v30_import_person_origin(uuid,uuid,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
