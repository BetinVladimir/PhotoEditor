-- +goose Up
-- Current editor-only review context. No object locations or inherited approval.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_review_context(p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_revision bigint,p_report_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;d photo.backend_documents%ROWTYPE;t photo.tasks%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange' AND renderer_version='v2.2';
 IF w.id IS NULL OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=w.run_id AND id=photo.v30_uuid5(w.id,'v2.interchange.import:0') FOR SHARE;
 IF r.status IS DISTINCT FROM 'waiting_review' OR r.revision IS DISTINCT FROM p_revision OR r.cancel_requested
 OR t.state IS DISTINCT FROM 'succeeded' OR photo.v30_interchange_import_adoption_committed(p_tenant,t.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND owner_id=t.id AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready' FOR SHARE;
 IF d.v30_import_review IS NULL OR d.v30_import_review#>>'{report,sha256}' IS DISTINCT FROM p_report_sha
 OR d.v30_import_review->>'workflow_id' IS DISTINCT FROM w.id::text OR d.v30_import_review->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN NULL;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(d.v30_import_review#>>'{report,artifact_id}')::uuid AND sha256=p_report_sha
 AND state='ready' AND verified_at IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('workflow_id',w.id,'project_id',w.project_id,'run_id',r.id,'revision',r.revision::text,'review',d.v30_import_review);
END $$;
ALTER FUNCTION photo.v30_interchange_review_context(uuid,uuid,uuid,uuid,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_review_context(uuid,uuid,uuid,uuid,bigint,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_review_context(uuid,uuid,uuid,uuid,bigint,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
