-- +goose Up
-- +goose StatementBegin
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text)
 RENAME TO v30_interchange_prepared_scope_without_background;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_prepared_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;occ jsonb;mapping jsonb;origin jsonb;file jsonb;certificate jsonb;
 ordinary uuid;expected uuid;entry uuid;style_id uuid;style_sha text;portable text;actual_bytes bigint;matches bigint;
BEGIN
 IF p_contract IS DISTINCT FROM 'StyleBackgroundAsset' THEN
  RETURN photo.v30_interchange_prepared_scope_without_background(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);
 END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_artifact IS NULL OR p_workflow IS NULL THEN RETURN NULL;END IF;
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 receipt:=p_body::jsonb;occ:=receipt->'background_occurrence';
 IF jsonb_typeof(receipt) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt))<>6
 OR NOT(receipt ?& ARRAY['entry_id','binding','sha256','bytes','media_type','background_occurrence'])
 OR jsonb_typeof(occ) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ))<>4
 OR NOT(occ ?& ARRAY['style','portable_asset_id','original','staged']) THEN RETURN NULL;END IF;
 IF jsonb_typeof(occ->'style') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ->'style'))<>2
 OR NOT(occ->'style' ?& ARRAY['artifact_id','sha256'])
 OR jsonb_typeof(occ->'original') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ->'original'))<>2
 OR NOT(occ->'original' ?& ARRAY['artifact_id','sha256'])
 OR jsonb_typeof(occ->'staged') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ->'staged'))<>2
 OR NOT(occ->'staged' ?& ARRAY['artifact_id','sha256']) THEN RETURN NULL;END IF;
 portable:=occ->>'portable_asset_id';style_id:=(occ#>>'{style,artifact_id}')::uuid;style_sha:=occ#>>'{style,sha256}';entry:=(receipt->>'entry_id')::uuid;
 IF jsonb_typeof(occ->'portable_asset_id') IS DISTINCT FROM 'string' OR octet_length(portable) NOT BETWEEN 1 AND 120
 OR coalesce(style_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(receipt->>'sha256','') !~ '^[0-9a-f]{64}$' OR receipt->>'sha256' IS DISTINCT FROM occ#>>'{original,sha256}'
 OR receipt->>'sha256' IS DISTINCT FROM occ#>>'{staged,sha256}'
 OR coalesce(receipt->>'media_type','') NOT IN('image/jpeg','image/png','image/tiff')
 OR jsonb_typeof(receipt->'bytes') IS DISTINCT FROM 'number' OR receipt->>'bytes' !~ '^[1-9][0-9]*$'
 OR (receipt->>'bytes')::bigint NOT BETWEEN 8 AND 536870912 THEN RETURN NULL;END IF;
 -- Reuse full live import/source/metadata/target authorization from the
 -- existing style dependency scope, with its independently derived identity.
 ordinary:=photo.v30_uuid5(p_workflow,'interchange:dependency:'||entry||':StyleV2:'||style_id);
 certificate:=photo.v30_interchange_prepared_scope_without_background(p_tenant,p_workflow,p_parsed,ordinary,'StyleV2',
  ((receipt-'background_occurrence')||jsonb_build_object('sha256',style_sha,'bytes',2,'media_type','application/json'))::text);
 IF certificate IS NULL OR certificate#>>'{origin,sha256}' IS DISTINCT FROM style_sha THEN RETURN NULL;END IF;
 SELECT count(*),min(value::text)::jsonb INTO matches,mapping
 FROM jsonb_array_elements(p_parsed::jsonb#>'{manifest,backgrounds}')
 WHERE value->'style'=occ->'style' AND value->>'portable_asset_id'=portable;
 IF matches<>1 THEN RETURN NULL;END IF;
 SELECT count(*),min(value::text)::jsonb INTO matches,origin
 FROM jsonb_array_elements(p_parsed::jsonb#>'{manifest,inventory}')
 WHERE value->>'path'=mapping->>'path' AND value->>'contract'='StyleBackgroundAsset'
 AND value->>'artifact_id'=occ#>>'{original,artifact_id}' AND value->>'sha256'=receipt->>'sha256';
 IF matches<>1 OR mapping->>'sha256' IS DISTINCT FROM receipt->>'sha256' THEN RETURN NULL;END IF;
 SELECT count(*),min(value::text)::jsonb INTO matches,file FROM jsonb_array_elements(p_parsed::jsonb->'files')
 WHERE value->>'path'=mapping->>'path' AND value->'content'=occ->'staged';
 IF matches<>1 THEN RETURN NULL;END IF;
 SELECT bytes INTO actual_bytes FROM photo.v30_interchange_import_file(p_tenant,p_workflow,p_parsed,
 (occ#>>'{staged,artifact_id}')::uuid,receipt->>'sha256');
 IF actual_bytes IS DISTINCT FROM (receipt->>'bytes')::bigint THEN RETURN NULL;END IF;
 expected:=photo.v30_uuid5(p_workflow,'interchange:background:'||
 (receipt#>>'{binding,asset_version_id}')||':'||(receipt#>>'{binding,source,artifact_id}')||':'||
 (receipt#>>'{binding,source,sha256}')||':'||(receipt#>>'{binding,canvas_hash}')||':'||style_id||':'||style_sha||':'||portable||':'||
 (occ#>>'{original,artifact_id}')||':'||(receipt->>'sha256'));
 IF expected IS NULL OR p_artifact IS DISTINCT FROM expected OR expected::text=portable THEN RETURN NULL;END IF;
 RETURN certificate||jsonb_build_object('owner',p_artifact,'sha',receipt->>'sha256','bytes',actual_bytes,
 'media_type',receipt->>'media_type','contract',p_contract,'origin',origin,'background_occurrence',occ);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
