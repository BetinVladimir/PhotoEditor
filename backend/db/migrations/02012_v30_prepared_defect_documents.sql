-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_interchange_prepared_scope_plain(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;receipt jsonb;source jsonb;original jsonb;file jsonb;origin jsonb;
 w photo.v2_workflows%ROWTYPE;entry_id uuid;size bigint;media text;sha text;matches integer;
BEGIN
 IF p_artifact IS NULL OR p_artifact='00000000-0000-0000-0000-000000000000'::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 IF p_contract IS NULL OR p_contract<>ALL(ARRAY['EditingRecipeV3','StyleV2','CorrectionPlan','Override','LayerStack','MaskRef','Params','Stats20','ResolvedStyle','DefectCandidates','DefectReview','MaskPixels','DenseWarpField','AbsoluteLUTArray']) THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);IF sources IS NULL THEN RETURN NULL;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);IF metadata IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 receipt:=p_body::jsonb;
 IF jsonb_typeof(receipt) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt))<>5
 OR NOT(receipt ?& ARRAY['entry_id','binding','sha256','bytes','media_type'])
 OR jsonb_typeof(receipt->'binding') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt->'binding'))<>3
 OR NOT(receipt->'binding' ?& ARRAY['asset_version_id','source','canvas_hash'])
 OR jsonb_typeof(receipt#>'{binding,source}') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt#>'{binding,source}'))<>2
 OR NOT(receipt#>'{binding,source}' ?& ARRAY['artifact_id','sha256'])
 OR coalesce(receipt#>>'{binding,canvas_hash}','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(receipt->'bytes') IS DISTINCT FROM 'number' OR receipt->>'bytes' !~ '^[1-9][0-9]*$'
 OR jsonb_typeof(receipt->'sha256') IS DISTINCT FROM 'string' OR coalesce(receipt->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 entry_id:=(receipt->>'entry_id')::uuid;size:=(receipt->>'bytes')::bigint;sha:=receipt->>'sha256';media:=receipt->>'media_type';
 IF p_contract IN('MaskPixels','DenseWarpField','AbsoluteLUTArray') THEN
  IF media IS DISTINCT FROM 'application/octet-stream' OR size>137438953472 THEN RETURN NULL;END IF;
 ELSE
  IF media IS DISTINCT FROM 'application/json' OR size NOT BETWEEN 2 AND 8388608 THEN RETURN NULL;END IF;
 END IF;
 SELECT value INTO source FROM jsonb_array_elements(sources->'entries') WHERE value->>'entry_id'=entry_id::text;
 IF source IS NULL OR source->>'state'<>'adopted' OR receipt#>>'{binding,asset_version_id}' IS DISTINCT FROM source->>'asset_version_id'
 OR receipt#>'{binding,source}' IS DISTINCT FROM source->'master' THEN RETURN NULL;END IF;
 SELECT value INTO original FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'entries') WHERE value->>'entry_id'=entry_id::text;
 IF original IS NULL OR original->'recipe_path' IS NULL OR original->'recipe_path'='null'::jsonb THEN RETURN NULL;END IF;
 IF p_contract='EditingRecipeV3' THEN
  IF p_artifact IS DISTINCT FROM photo.v30_uuid5(p_workflow,'interchange:recipe:'||entry_id) THEN RETURN NULL;END IF;
  SELECT value INTO file FROM jsonb_array_elements(p_parsed::jsonb->'files') WHERE value->>'path'=original->>'recipe_path';
  origin:=jsonb_build_object('path',original->>'recipe_path','sha256',original->>'recipe_sha256');
 ELSE
  -- Ordinary dependency identities are derived independently from immutable
  -- inventory. Occurrence-specific warp certificates need their own extension.
  SELECT count(*) INTO matches FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'inventory') item
  WHERE item->>'contract'=p_contract AND photo.v30_uuid5(p_workflow,'interchange:dependency:'||entry_id||':'||p_contract||':'||(item->>'artifact_id'))=p_artifact;
  IF matches<>1 THEN RETURN NULL;END IF;
  SELECT item INTO origin FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'inventory') item
  WHERE item->>'contract'=p_contract AND photo.v30_uuid5(p_workflow,'interchange:dependency:'||entry_id||':'||p_contract||':'||(item->>'artifact_id'))=p_artifact;
  SELECT value INTO file FROM jsonb_array_elements(p_parsed::jsonb->'files') WHERE value->>'path'=origin->>'path';
  IF p_contract IN('MaskPixels','DenseWarpField','AbsoluteLUTArray') AND sha IS DISTINCT FROM origin->>'sha256' THEN RETURN NULL;END IF;
 END IF;
 IF file IS NULL OR file#>>'{content,sha256}' IS DISTINCT FROM origin->>'sha256' THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_import_file(p_tenant,p_workflow,p_parsed,(file#>>'{content,artifact_id}')::uuid,file#>>'{content,sha256}');
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',w.run_id,'owner',p_artifact,'sha',sha,'bytes',size,'media_type',media,'contract',p_contract,
 'workflow',p_workflow,'entry',entry_id,'binding',receipt->'binding','origin',origin,'parsed',sources->'parsed',
 'sources_digest',encode(sha256(convert_to(sources::text,'UTF8')),'hex'),'metadata_digest',encode(sha256(convert_to(metadata::text,'UTF8')),'hex'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
