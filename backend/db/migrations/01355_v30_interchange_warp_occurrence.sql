-- +goose Up
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_prepared_scope_plain;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_canvas_jcs(p_canvas jsonb) RETURNS text
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE width bigint;height bigint;
BEGIN
 IF jsonb_typeof(p_canvas) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_canvas))<>5
 OR NOT(p_canvas ?& ARRAY['width','height','orientation','space_id','source_sha256'])
 OR jsonb_typeof(p_canvas->'width') IS DISTINCT FROM 'number' OR p_canvas->>'width' !~ '^[1-9][0-9]*$'
 OR jsonb_typeof(p_canvas->'height') IS DISTINCT FROM 'number' OR p_canvas->>'height' !~ '^[1-9][0-9]*$'
 OR p_canvas->'orientation' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(p_canvas->'space_id') IS DISTINCT FROM 'string' OR coalesce(p_canvas->>'space_id','') !~ '^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$'
 OR jsonb_typeof(p_canvas->'source_sha256') IS DISTINCT FROM 'string' OR coalesce(p_canvas->>'source_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 width:=(p_canvas->>'width')::bigint;height:=(p_canvas->>'height')::bigint;
 IF width>200000000 OR height>200000000/width THEN RETURN NULL;END IF;
 RETURN format('{"height":%s,"orientation":1,"source_sha256":"%s","space_id":"%s","width":%s}',height,p_canvas->>'source_sha256',p_canvas->>'space_id',width);
EXCEPTION WHEN numeric_value_out_of_range OR invalid_text_representation THEN RETURN NULL;
END $$;
REVOKE ALL ON FUNCTION photo.v30_import_canvas_jcs(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_canvas_jcs(jsonb) TO photo_fn_owner;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;warp jsonb;certificate jsonb;ordinary uuid;original uuid;entry uuid;
 from_jcs text;to_jcs text;target_jcs text;occurrence text;image_sha text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 receipt:=p_body::jsonb;
 IF NOT(receipt ? 'warp_occurrence') THEN RETURN photo.v30_interchange_prepared_scope_plain(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);END IF;
 IF p_contract IS DISTINCT FROM 'DenseWarpField' OR jsonb_typeof(receipt) IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(receipt))<>6 THEN RETURN NULL;END IF;
 warp:=receipt->'warp_occurrence';
 IF jsonb_typeof(warp) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(warp))<>3
 OR NOT(warp ?& ARRAY['original','from_canvas','to_canvas'])
 OR jsonb_typeof(warp->'original') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(warp->'original'))<>2
 OR NOT(warp->'original' ?& ARRAY['artifact_id','sha256'])
 OR warp#>>'{original,sha256}' IS DISTINCT FROM receipt->>'sha256' THEN RETURN NULL;END IF;
 original:=(warp#>>'{original,artifact_id}')::uuid;entry:=(receipt->>'entry_id')::uuid;
 from_jcs:=photo.v30_import_canvas_jcs(warp->'from_canvas');to_jcs:=photo.v30_import_canvas_jcs(warp->'to_canvas');
 IF from_jcs IS NULL OR to_jcs IS NULL THEN RETURN NULL;END IF;
 SELECT value->>'image_sha256' INTO image_sha FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'entries') WHERE value->>'entry_id'=entry::text;
 IF image_sha IS NULL OR warp#>>'{from_canvas,source_sha256}' IS DISTINCT FROM image_sha
 OR warp#>>'{to_canvas,source_sha256}' IS DISTINCT FROM image_sha THEN RETURN NULL;END IF;
 ordinary:=photo.v30_uuid5(p_workflow,'interchange:dependency:'||entry||':DenseWarpField:'||original);
 certificate:=photo.v30_interchange_prepared_scope_plain(p_tenant,p_workflow,p_parsed,ordinary,p_contract,(receipt-'warp_occurrence')::text);
 IF certificate IS NULL THEN RETURN NULL;END IF;
 target_jcs:=format('{"asset_version_id":"%s","canvas_hash":"%s","source":{"artifact_id":"%s","sha256":"%s"}}',
 receipt#>>'{binding,asset_version_id}',receipt#>>'{binding,canvas_hash}',receipt#>>'{binding,source,artifact_id}',receipt#>>'{binding,source,sha256}');
 occurrence:=encode(sha256(convert_to('{"From":'||from_jcs||',"Target":'||target_jcs||',"To":'||to_jcs||'}','UTF8')),'hex');
 IF p_artifact IS DISTINCT FROM photo.v30_uuid5(p_workflow,'interchange:warp:'||entry||':'||original||':'||occurrence) THEN RETURN NULL;END IF;
 RETURN certificate||jsonb_build_object('owner',p_artifact,'warp_occurrence',warp);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_v30_interchange_prepared(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text,p_bucket text)
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
CREATE OR REPLACE FUNCTION photo.complete_v30_interchange_prepared(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text,p_bucket text,p_token uuid,p_version text) RETURNS boolean
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_interchange_prepared_document(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;w photo.v2_workflows%ROWTYPE;certificate jsonb;receipt jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_artifact IS NULL OR p_entry IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 -- Match the lock order of reserve/complete before authority helpers acquire locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=w.run_id
 AND b.owner_id=p_artifact AND b.artifact_id=p_artifact AND b.purpose='v30_import_prepared';
 IF NOT FOUND OR d.state<>'ready' OR d.sha256 IS DISTINCT FROM p_sha
 OR d.v30_import_prepared->>'contract' IS DISTINCT FROM p_contract
 OR d.v30_import_prepared->>'entry' IS DISTINCT FROM p_entry::text
 OR d.v30_import_prepared->'binding' IS DISTINCT FROM p_binding THEN RETURN;END IF;
 receipt:=jsonb_build_object('entry_id',p_entry,'binding',p_binding,'sha256',d.sha256,'bytes',d.bytes,'media_type',d.v30_import_prepared->>'media_type');
 IF d.v30_import_prepared ? 'warp_occurrence' THEN receipt:=receipt||jsonb_build_object('warp_occurrence',d.v30_import_prepared->'warp_occurrence');END IF;
 certificate:=photo.v30_interchange_prepared_scope(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,receipt::text);
 IF certificate IS NULL OR certificate IS DISTINCT FROM d.v30_import_prepared THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.output_role='v30_import_prepared' AND a.object_version NOT IN('','null')
 AND a.media_type=certificate->>'media_type'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||w.run_id||'/documents/'||p_artifact
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
