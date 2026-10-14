-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_review_parsed(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_revision bigint,p_report_sha text)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;
BEGIN
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha);
 IF authority IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
 WHERE t.tenant_id=p_tenant AND t.run_id=(authority->>'run_id')::uuid
 AND t.operation='v2.interchange.parse' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND photo.v30_interchange_parse_adoption_committed(p_tenant,t.id) IS TRUE
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF t,a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_review_parsed(uuid,uuid,uuid,uuid,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_review_parsed(uuid,uuid,uuid,uuid,bigint,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_review_parsed(uuid,uuid,uuid,uuid,bigint,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
