-- +goose Up
-- Storage authorization only; closure/type qualification and final adoption remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_document_scope(p_tenant uuid,p_workflow uuid,p_parsed text,
 p_artifact uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;metadata jsonb;body jsonb;parsed jsonb;entry jsonb;source jsonb;original jsonb;file jsonb;
 w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;
 task uuid;expected_id uuid;plan_id uuid;n integer:=0;sha text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_purpose NOT IN('v30_interchange_import_plan','task_params') THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN NULL;END IF;
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,p_workflow,p_parsed);
 IF metadata IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id;
 task:=photo.v30_uuid5(p_workflow,'v2.interchange.import:0');
 plan_id:=photo.v30_uuid5(task,'interchange:plan:1');
 body:=p_body::jsonb;parsed:=p_parsed::jsonb;sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 IF p_purpose='task_params' THEN
  expected_id:=photo.v30_uuid5(task,'interchange:params:1');
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=w.run_id AND owner_id=task
   AND purpose='v30_interchange_import_plan' AND artifact_id=plan_id AND state='ready';
  IF d.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=plan_id
   AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.bucket=d.bucket AND a.object_version=d.object_version AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
  IF body IS DISTINCT FROM jsonb_build_object('schema_version',1,'parsed',sources->'parsed',
    'plan',jsonb_build_object('artifact_id',plan_id,'sha256',d.sha256),'max_report_bytes',8388608) THEN RETURN NULL;END IF;
 ELSE
  expected_id:=plan_id;
  IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(body))<>9
   OR NOT (body ?& ARRAY['schema_version','project_id','direction','format','snapshot_digest','entries','max_archive_bytes','max_expanded_bytes','max_files'])
   OR body->>'schema_version' IS DISTINCT FROM '1' OR body->>'project_id' IS DISTINCT FROM w.project_id::text
   OR body->>'direction' IS DISTINCT FROM 'import' OR body->>'format' IS DISTINCT FROM 'photoeditor_bundle'
   OR body->>'snapshot_digest' IS DISTINCT FROM r.config_snapshot->>'snapshot_digest'
   OR coalesce((body->>'max_archive_bytes')::bigint,0) NOT BETWEEN 1 AND 68719476736
   OR coalesce((body->>'max_expanded_bytes')::bigint,0) NOT BETWEEN 1 AND 137438953472
   OR coalesce((body->>'max_files')::integer,0) NOT BETWEEN 1 AND 30000
   OR jsonb_typeof(body->'entries') IS DISTINCT FROM 'array'
   OR jsonb_array_length(body->'entries')<>jsonb_array_length(sources->'entries') THEN RETURN NULL;END IF;
  FOR entry IN SELECT value FROM jsonb_array_elements(body->'entries') LOOP
   source:=sources->'entries'->n;original:=parsed->'manifest'->'entries'->n;
   SELECT value INTO file FROM jsonb_array_elements(parsed->'files') WHERE value->>'path'=original->>'image_path';
   IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(entry))<>9
    OR NOT (entry ?& ARRAY['entry_id','external_id','path','content','binding','recipe','rating','pick','crop'])
    OR entry->'entry_id' IS DISTINCT FROM original->'entry_id' OR entry->'external_id' IS DISTINCT FROM original->'external_id'
    OR entry->'path' IS DISTINCT FROM original->'image_path' OR entry->'content' IS DISTINCT FROM file->'content'
    OR entry->'rating' IS DISTINCT FROM original->'rating' OR entry->'pick' IS DISTINCT FROM original->'pick'
    OR entry->'crop' IS DISTINCT FROM original->'crop' THEN RETURN NULL;END IF;
   IF source->>'state'='adopted' THEN
    IF jsonb_typeof(entry->'binding') IS DISTINCT FROM 'object'
     OR (SELECT count(*) FROM jsonb_object_keys(entry->'binding'))<>3
     OR entry#>'{binding,asset_version_id}' IS DISTINCT FROM source->'asset_version_id'
     OR entry#>'{binding,source}' IS DISTINCT FROM source->'master'
     OR coalesce(entry#>>'{binding,canvas_hash}','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
    IF entry->'recipe'<>'null'::jsonb AND (original->'recipe_path'='null'::jsonb
     OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
      AND a.id=(entry#>>'{recipe,artifact_id}')::uuid AND a.sha256=entry#>>'{recipe,sha256}'
      AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
      AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN NULL;END IF;
   ELSIF entry->'binding' IS DISTINCT FROM 'null'::jsonb OR entry->'recipe' IS DISTINCT FROM 'null'::jsonb THEN RETURN NULL;
   END IF;
   n:=n+1;
  END LOOP;
 END IF;
 IF p_artifact IS DISTINCT FROM expected_id THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',w.run_id,'owner',task,'sha',sha,'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_document_scope(uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
