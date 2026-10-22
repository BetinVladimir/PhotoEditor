-- +goose Up
-- A PNG occurrence retains exact archived report membership. Physical PNG/NPY
-- equality and original producer qualification are mandatory application gates.
-- +goose StatementBegin
DO $$
DECLARE definition text;anchor text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_prepared_scope_plain(uuid,uuid,text,uuid,text,text)'::regprocedure);
 anchor:='''AbsoluteLUTArray''';
 IF definition IS NULL OR strpos(definition,'SECURITY DEFINER')=0
 OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>3 THEN
  RAISE EXCEPTION 'IMPORT_PNG_PLAIN_SCOPE_SHAPE';
 END IF;
 -- 02016 replaced the plain function and accidentally omitted DefectLabelMap.
 -- Restore that binary role alongside PNG; both occurrence wrappers still apply.
 definition:=replace(definition,anchor,anchor||','||quote_literal('DefectLabelMap')||','||quote_literal('DefectLabelPNG'));
 anchor:='IF p_contract IN(';
 -- PNG has its own media branch; all original JSON/array validation remains.
 anchor:=' IF p_contract IN(''MaskPixels'',''DenseWarpField'',''AbsoluteLUTArray'',''DefectLabelMap'',''DefectLabelPNG'') THEN';
 IF (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'IMPORT_PNG_MEDIA_SCOPE_SHAPE';END IF;
 definition:=replace(definition,' IF p_contract IN(''MaskPixels'',''DenseWarpField'',''AbsoluteLUTArray'',''DefectLabelMap'',''DefectLabelPNG'') THEN',
 ' IF p_contract=''DefectLabelPNG'' THEN
  IF media IS DISTINCT FROM ''image/png'' OR size NOT BETWEEN 8 AND 536870912 THEN RETURN NULL;END IF;
 ELSIF p_contract IN(''MaskPixels'',''DenseWarpField'',''AbsoluteLUTArray'',''DefectLabelMap'') THEN');
 EXECUTE definition;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) RENAME TO v30_interchange_prepared_scope_without_defect_png;
CREATE FUNCTION photo.v30_interchange_prepared_scope(p_tenant uuid,p_workflow uuid,p_parsed text,p_artifact uuid,p_contract text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;occ jsonb;report jsonb;item jsonb;file jsonb;certificate jsonb;matches integer;seen uuid[]:=ARRAY[]::uuid[];report_id uuid;actual_bytes bigint;
BEGIN
 IF p_contract IS DISTINCT FROM 'DefectLabelPNG' THEN
  RETURN photo.v30_interchange_prepared_scope_without_defect_png(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,p_body);
 END IF;
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 receipt:=p_body::jsonb;occ:=receipt->'defect_png_occurrence';
 IF jsonb_typeof(receipt) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(receipt))<>6
 OR NOT(receipt ?& ARRAY['entry_id','binding','sha256','bytes','media_type','defect_png_occurrence'])
 OR jsonb_typeof(occ) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ))<>2
 OR NOT(occ ?& ARRAY['original','reports'])
 OR jsonb_typeof(occ->'original') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(occ->'original'))<>2
 OR NOT(occ->'original' ?& ARRAY['artifact_id','sha256'])
 OR occ#>>'{original,sha256}' IS DISTINCT FROM receipt->>'sha256'
 OR receipt->>'media_type' IS DISTINCT FROM 'image/png'
 OR jsonb_typeof(occ->'reports') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF jsonb_array_length(occ->'reports') NOT BETWEEN 1 AND 128 THEN RETURN NULL;END IF;
 certificate:=photo.v30_interchange_prepared_scope_without_defect_png(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,(receipt-'defect_png_occurrence')::text);
 IF certificate IS NULL OR certificate#>>'{origin,artifact_id}' IS DISTINCT FROM occ#>>'{original,artifact_id}' THEN RETURN NULL;END IF;
 FOR report IN SELECT value FROM jsonb_array_elements(occ->'reports') LOOP
  IF jsonb_typeof(report) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(report))<>2
  OR NOT(report ?& ARRAY['artifact_id','sha256']) OR coalesce(report->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
  report_id:=(report->>'artifact_id')::uuid;
  IF report_id='00000000-0000-0000-0000-000000000000'::uuid OR report_id=ANY(seen)
  OR report->>'artifact_id'=occ#>>'{original,artifact_id}' THEN RETURN NULL;END IF;
  seen:=array_append(seen,report_id);
  SELECT count(*),min(value::text)::jsonb INTO matches,item FROM jsonb_array_elements(p_parsed::jsonb#>'{manifest,inventory}')
  WHERE value->>'artifact_id'=report_id::text AND value->>'sha256'=report->>'sha256' AND value->>'contract'='DefectProposeReport';
  IF matches<>1 THEN RETURN NULL;END IF;
  SELECT count(*),min(value::text)::jsonb INTO matches,file FROM jsonb_array_elements(p_parsed::jsonb->'files')
  WHERE value->>'path'=item->>'path' AND value->>'contract'='DefectProposeReport' AND value#>>'{content,sha256}'=report->>'sha256';
  IF matches<>1 THEN RETURN NULL;END IF;
  PERFORM 1 FROM photo.v30_interchange_import_file(p_tenant,p_workflow,p_parsed,(file#>>'{content,artifact_id}')::uuid,report->>'sha256');
  IF NOT FOUND THEN RETURN NULL;END IF;
 END LOOP;
 SELECT value INTO file FROM jsonb_array_elements(p_parsed::jsonb->'files') WHERE value->>'path'=certificate#>>'{origin,path}';
 IF file IS NULL THEN RETURN NULL;END IF;
 SELECT bytes INTO actual_bytes FROM photo.v30_interchange_import_file(p_tenant,p_workflow,p_parsed,(file#>>'{content,artifact_id}')::uuid,receipt->>'sha256');
 IF actual_bytes IS DISTINCT FROM (receipt->>'bytes')::bigint THEN RETURN NULL;END IF;
 RETURN certificate||jsonb_build_object('defect_png_occurrence',occ);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$
DECLARE definition text;anchor text:=' certificate:=photo.v30_interchange_prepared_scope(';
BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text)'::regprocedure);
 IF definition IS NULL OR (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN RAISE EXCEPTION 'IMPORT_PNG_DOCUMENT_SHAPE';END IF;
 definition:=replace(definition,anchor,' IF d.v30_import_prepared ? ''defect_png_occurrence'' THEN receipt:=receipt||jsonb_build_object(''defect_png_occurrence'',d.v30_import_prepared->''defect_png_occurrence'');END IF;'||E'\n'||anchor);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
