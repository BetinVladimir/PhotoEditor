-- +goose Up
-- The API may read only an already published proposal for the current import
-- entry and newly ingested version. This is display evidence, not a grant.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_review_proposal_object(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_target_version uuid,p_kind text,p_proposal uuid,p_parsed text)
RETURNS TABLE(proposal_sha text,bucket text,object_key text,object_version text,
 bytes bigint,media_type text,base_id uuid,base_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;sources jsonb;source jsonb;
 d photo.backend_documents%ROWTYPE;a photo.artifacts%ROWTYPE;scope jsonb;contract text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_entry IS NULL OR p_target_version IS NULL
 OR p_proposal IS NULL OR p_kind IS NULL OR p_kind NOT IN('defects','dust')
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608 THEN RETURN;END IF;
 contract:=CASE WHEN p_kind='defects' THEN 'DefectCandidates' ELSE 'DustCandidates' END;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN;END IF;
 SELECT value INTO source FROM jsonb_array_elements(sources->'entries')
 WHERE value->>'entry_id'=p_entry::text AND value->>'state'='adopted'
 AND value->>'asset_version_id'=p_target_version::text;
 IF source IS NULL OR source#>>'{master,artifact_id}' IS NULL
 OR coalesce(source#>>'{master,sha256}','') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=w.run_id AND b.artifact_id=p_proposal AND b.purpose='v30_import_prepared'
  AND b.state='ready' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 scope:=d.v30_import_prepared;
 IF scope IS NULL OR scope->>'contract' IS DISTINCT FROM contract
 OR scope->>'workflow' IS DISTINCT FROM p_workflow::text
 OR scope->>'entry' IS DISTINCT FROM p_entry::text
 OR scope#>>'{binding,asset_version_id}' IS DISTINCT FROM p_target_version::text
 OR scope#>'{binding,source}' IS DISTINCT FROM source->'master'
 OR scope->>'sha' IS DISTINCT FROM d.sha256 THEN RETURN;END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND x.id=p_proposal
  AND x.state='ready' AND x.sha256=d.sha256 AND x.bytes=d.bytes
  AND x.bucket=d.bucket AND x.object_version=d.object_version
  AND x.output_role='v30_import_prepared' AND x.media_type='application/json'
  AND x.verified_at IS NOT NULL AND x.object_version NOT IN('','null')
  AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR a.bytes NOT BETWEEN 2 AND 1048576 THEN RETURN;END IF;
 RETURN QUERY SELECT a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type,
  (source#>>'{master,artifact_id}')::uuid,source#>>'{master,sha256}';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_import_review_proposal_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_proposal_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_proposal_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
