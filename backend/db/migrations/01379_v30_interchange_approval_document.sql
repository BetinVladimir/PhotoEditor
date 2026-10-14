-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_approval_document(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_revision bigint,p_report_sha text,
 p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;target jsonb;project uuid;
BEGIN
 IF p_contract IS NULL OR p_contract NOT IN('StyleV2','CorrectionPlan','Override','LayerStack','MaskRef','Params') THEN RETURN;END IF;
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha);
 IF authority IS NULL THEN RETURN;END IF;
 project:=(authority->>'project_id')::uuid;
 SELECT value INTO target FROM jsonb_array_elements(authority#>'{review,targets}') WHERE value->>'entry_id'=p_entry::text;
 IF target IS NULL OR target->>'state' IS DISTINCT FROM 'proposed'
 OR target->>'asset_version_id' IS DISTINCT FROM p_binding->>'asset_version_id' THEN RETURN;END IF;
 PERFORM s.id FROM photo.v2_document_streams s JOIN photo.v2_documents d
 ON d.tenant_id=s.tenant_id AND d.project_id=s.project_id AND d.stream_id=s.id AND d.id=s.current_document_id
 WHERE s.tenant_id=p_tenant AND s.project_id=project AND s.id=(target->>'stream_id')::uuid
 AND s.asset_version_id=(target->>'asset_version_id')::uuid AND s.stream_type='draft'
 AND s.current_document_id=(target->>'document_id')::uuid AND s.approved_document_id IS NULL
 AND d.artifact_id=(target#>>'{recipe,artifact_id}')::uuid AND d.sha256=target#>>'{recipe,sha256}' FOR SHARE OF s,d;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT x.bucket,x.object_key,x.object_version,x.sha256,x.bytes,x.media_type
 FROM photo.v30_interchange_prepared_document(p_tenant,p_workflow,p_parsed,p_entry,p_binding,p_artifact,p_sha,p_contract) x
 WHERE x.media_type='application/json' AND x.bytes BETWEEN 2 AND 8388608;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_approval_document(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_approval_document(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_approval_document(uuid,uuid,uuid,uuid,bigint,text,text,uuid,jsonb,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
