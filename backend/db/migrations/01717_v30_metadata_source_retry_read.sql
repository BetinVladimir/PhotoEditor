-- +goose Up
-- API-only recovery of normalized bytes; no original object location is returned.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_metadata_source_retry(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,
 p_metadata uuid,p_binding jsonb,p_policy text,p_approved text[])
RETURNS TABLE(outcome text,publication_jcs text,publication_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;envelope jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.created_by=p_actor AND r.kind='v2'
 AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND w.project_id=p_project AND w.workflow_type='delivery' AND w.renderer_version='v2.2') THEN RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND purpose='v30_metadata_source' AND artifact_id=p_metadata;
 IF NOT FOUND THEN RETURN QUERY SELECT 'absent'::text,NULL::text,NULL::text;RETURN;END IF;
 IF d.state NOT IN('pending','ready') OR d.v30_metadata_source_jcs IS NULL THEN RETURN;END IF;
 envelope:=d.v30_metadata_source_jcs::jsonb;
 IF envelope->'scope'->'binding' IS DISTINCT FROM p_binding
 OR envelope->'metadata_source'->>'artifact_id' IS DISTINCT FROM p_metadata::text
 OR photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,d.v30_metadata_source_jcs,
 d.v30_metadata_source_sha,p_policy,p_approved) IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT 'saved'::text,d.v30_metadata_source_jcs,d.v30_metadata_source_sha;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[])
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_metadata_source_retry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text,text[]) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
