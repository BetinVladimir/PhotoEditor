-- +goose Up
-- +goose StatementBegin
ALTER TABLE photo.backend_documents ADD COLUMN v30_import_prepared jsonb;
-- +goose StatementEnd
-- Prepared import artifacts remain unapproved; typed closure checks are separate.
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_import_prepared'')',expression);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;receipt jsonb;source jsonb;original jsonb;file jsonb;origin jsonb;
 w photo.v2_workflows%ROWTYPE;entry_id uuid;size bigint;media text;sha text;matches integer;
BEGIN
 IF p_artifact IS NULL OR p_artifact='00000000-0000-0000-0000-000000000000'::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 IF p_contract IS NULL OR p_contract<>ALL(ARRAY['EditingRecipeV3','StyleV2','CorrectionPlan','Override','LayerStack','MaskRef','Params','MaskPixels','DenseWarpField']) THEN RETURN NULL;END IF;
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
 IF p_contract IN('MaskPixels','DenseWarpField') THEN
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
  IF p_contract IN('MaskPixels','DenseWarpField') AND sha IS DISTINCT FROM origin->>'sha256' THEN RETURN NULL;END IF;
 END IF;
 IF file IS NULL OR file#>>'{content,sha256}' IS DISTINCT FROM origin->>'sha256' THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v30_interchange_import_file(p_tenant,p_workflow,p_parsed,(file#>>'{content,artifact_id}')::uuid,file#>>'{content,sha256}');
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',w.run_id,'owner',p_artifact,'sha',sha,'bytes',size,'media_type',media,'contract',p_contract,
 'workflow',p_workflow,'entry',entry_id,'binding',receipt->'binding','origin',origin,'parsed',sources->'parsed',
 'sources_digest',encode(sha256(convert_to(sources::text,'UTF8')),'hex'),'metadata_digest',encode(sha256(convert_to(metadata::text,'UTF8')),'hex'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_interchange_prepared(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;sha text;size bigint;fence uuid:=gen_random_uuid();used numeric;capacity numeric;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 -- Serialize quota/document writers before the scope helper takes shared locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 scope:=photo.v30_interchange_prepared_scope(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);
 IF scope IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;sha:=scope->>'sha';size:=(scope->>'bytes')::bigint;
 IF size>137438953472 THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='running' AND graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose='v30_import_prepared' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>sha OR d.bytes<>size OR d.bucket<>p_bucket OR d.v30_import_prepared IS DISTINCT FROM scope THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready'
    AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v30_import_prepared)
 VALUES(p_tenant,rid,owner,'v30_import_prepared',p_artifact,fence,sha,size,p_bucket,scope);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v30_interchange_prepared(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text,p_bucket text,p_token uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 scope:=photo.v30_interchange_prepared_scope(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='running' AND graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose='v30_import_prepared' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM scope->>'sha'
 OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint OR d.bucket IS DISTINCT FROM p_bucket OR d.v30_import_prepared IS DISTINCT FROM scope THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=p_version AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id,p_version,d.sha256,d.bytes,scope->>'media_type','ready',clock_timestamp(),'v30_import_prepared');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_interchange_prepared(uuid,uuid,text,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
