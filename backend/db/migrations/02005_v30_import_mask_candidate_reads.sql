-- +goose Up
-- API-only locators for a current import's immutable parsed manifest and
-- bounded archive JSON. Object bytes stay behind server-side object reads.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_mask_parsed_object(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;ref jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status NOT IN('running','waiting_review') OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id
 AND operation='v2.interchange.parse' AND operation_version='1'
 AND state='succeeded' AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR photo.v30_interchange_parse_adoption_committed(p_tenant,t.id) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 IF NOT FOUND THEN RETURN;END IF;
 ref:=result.outputs->0;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid
 AND a.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_mask_parsed_object(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_mask_parsed_object(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_mask_parsed_object(uuid,uuid,uuid,uuid) TO photo_api;

CREATE FUNCTION photo.v30_import_mask_archive_document(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed text,
 p_staged uuid,p_sha text,p_contract text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;sources jsonb;parsed jsonb;file jsonb;item jsonb;n integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR p_contract NOT IN('EditingRecipeV3','Override') THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 parsed:=p_parsed::jsonb;
 SELECT count(*),jsonb_agg(value)->0 INTO n,file FROM jsonb_array_elements(parsed->'files')
 WHERE value#>>'{content,artifact_id}'=p_staged::text AND value#>>'{content,sha256}'=p_sha
 AND value->>'contract'=p_contract;
 IF n<>1 THEN RETURN;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO n,item FROM jsonb_array_elements(parsed->'manifest'->'inventory')
 WHERE value->>'path'=file->>'path' AND value->>'sha256'=p_sha AND value->>'contract'=p_contract;
 IF n<>1 THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_staged AND a.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='imported_file'
 AND a.media_type='application/octet-stream' AND a.bytes BETWEEN 2 AND CASE WHEN p_contract='EditingRecipeV3' THEN 1048576 ELSE 8388608 END
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_mask_archive_document(uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_mask_archive_document(uuid,uuid,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_mask_archive_document(uuid,uuid,uuid,uuid,text,uuid,text,text)
 TO photo_api;

-- Resolve the new asset version from adopted ingest state, never from the
-- client's proposed decision body. A grant requires the parent still running.
CREATE FUNCTION photo.v30_import_mask_candidate_target(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed text,p_entry uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;candidate jsonb;n integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR p_entry IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO n,candidate FROM jsonb_array_elements(sources->'entries')
 WHERE value->>'entry_id'=p_entry::text AND value->>'state'='adopted';
 IF n<>1 OR candidate->>'asset_version_id' IS NULL THEN RETURN NULL;END IF;
 RETURN (candidate->>'asset_version_id')::uuid;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_mask_candidate_target(uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_mask_candidate_target(uuid,uuid,uuid,uuid,text,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_mask_candidate_target(uuid,uuid,uuid,uuid,text,uuid) TO photo_api;

CREATE FUNCTION photo.v30_import_mask_candidate_entries(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed text)
RETURNS TABLE(entry_id uuid,asset_version_id uuid,asset_id uuid,external_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608 THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 RETURN QUERY SELECT (s.value->>'entry_id')::uuid,(s.value->>'asset_version_id')::uuid,
  v.asset_id,e.value->>'external_id' FROM jsonb_array_elements(sources->'entries') s(value)
 JOIN jsonb_array_elements(p_parsed::jsonb->'manifest'->'entries') e(value)
 ON e.value->>'entry_id'=s.value->>'entry_id'
 JOIN photo.asset_versions v ON v.tenant_id=p_tenant AND v.id=(s.value->>'asset_version_id')::uuid
 JOIN photo.assets asset ON asset.tenant_id=p_tenant AND asset.id=v.asset_id AND asset.project_id=w.project_id AND asset.deleted_at IS NULL
 WHERE s.value->>'state'='adopted' AND e.value->>'recipe_path' IS NOT NULL
 ORDER BY (s.value->>'ordinal')::integer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_mask_candidate_entries(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_mask_candidate_entries(uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_mask_candidate_entries(uuid,uuid,uuid,uuid,text) TO photo_api;

CREATE FUNCTION photo.v30_import_mask_pixel_object(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_parsed text,p_original uuid,p_staged uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;sources jsonb;parsed jsonb;item jsonb;file jsonb;n integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 parsed:=p_parsed::jsonb;
 SELECT count(*),jsonb_agg(value)->0 INTO n,item FROM jsonb_array_elements(parsed->'manifest'->'inventory')
 WHERE value->>'artifact_id'=p_original::text AND value->>'sha256'=p_sha AND value->>'contract'='MaskPixels';
 IF n<>1 THEN RETURN;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO n,file FROM jsonb_array_elements(parsed->'files')
 WHERE value->>'path'=item->>'path' AND value->>'contract'='MaskPixels'
 AND value#>>'{content,artifact_id}'=p_staged::text AND value#>>'{content,sha256}'=p_sha;
 IF n<>1 THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_staged AND a.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='imported_file'
 AND a.media_type='application/octet-stream' AND a.bytes BETWEEN 10 AND 800065548
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_mask_pixel_object(uuid,uuid,uuid,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_mask_pixel_object(uuid,uuid,uuid,uuid,text,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_mask_pixel_object(uuid,uuid,uuid,uuid,text,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
