-- +goose Up
-- Extend the existing ACL-filtered inspection with bounded review summary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.inspect_v30_interchange(p_tenant uuid,p_actor uuid,p_session uuid,p_job uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT base.value||jsonb_build_object('review',CASE WHEN d.v30_import_review IS NULL THEN NULL ELSE
 jsonb_build_object('report',d.v30_import_review->'report','proposed_count',
 (SELECT count(*) FROM jsonb_array_elements(d.v30_import_review->'targets') t WHERE t->>'state'='proposed'),
 'can_decide',base.value->>'status'='waiting_review' AND photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session),
 'targets',coalesce((SELECT jsonb_agg(jsonb_build_object('entry_id',value->'entry_id','external_id',value->'external_id',
 'asset_version_id',value->'asset_version_id','state',value->'state','fields',value->'fields') ORDER BY ord)
 FROM (SELECT value,ord FROM jsonb_array_elements(d.v30_import_review->'targets') WITH ORDINALITY t(value,ord)
 WHERE value->>'state'='proposed' ORDER BY ord LIMIT 100) page),'[]'::jsonb),
 'entry_count',jsonb_array_length(d.v30_import_review->'targets'),
 'can_close',base.value->>'status'='waiting_review'
 AND photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(d.v30_import_review->'targets') t WHERE t->>'state'='proposed')) END)
 FROM (SELECT photo.inspect_v30_interchange_pre_review(p_tenant,p_actor,p_session,p_job) AS value) base
 JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.id=p_job
 LEFT JOIN photo.tasks task ON task.tenant_id=w.tenant_id AND task.run_id=w.run_id AND task.id=photo.v30_uuid5(w.id,'v2.interchange.import:0')
 LEFT JOIN photo.backend_documents d ON d.tenant_id=task.tenant_id AND d.owner_id=task.id AND d.artifact_id=task.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 WHERE base.value IS NOT NULL
$$;
ALTER FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
