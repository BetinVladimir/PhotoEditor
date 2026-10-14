-- +goose Up
-- Scheduler read authority for independent import reconstruction. No adoption mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_import_adoption_context(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE;e photo.backend_documents%ROWTYPE;sources jsonb;metadata jsonb;budget bigint;
BEGIN
 IF p_tenant IS NULL OR p_task IS NULL OR p_result IS NULL OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 THEN RETURN NULL;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND workflow_type='interchange' AND renderer_version='v2.2';
 IF t.id IS NULL OR r.id IS NULL OR w.id IS NULL OR t.id<>photo.v30_uuid5(w.id,'v2.interchange.import:0')
 OR t.operation<>'v2.interchange.import' OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.state<>'succeeded' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
 OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task)) THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=p_task
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready' FOR SHARE;
 SELECT * INTO e FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=p_task
 AND purpose='v30_interchange_import_evidence' AND artifact_id=photo.v30_uuid5(p_task,'interchange:evidence:1') AND state='ready' FOR SHARE;
 IF d.id IS NULL OR e.id IS NULL OR e.v30_import_evidence IS NULL OR e.v30_import_parsed_jcs IS NULL
 OR d.v30_import_evidence_ref IS DISTINCT FROM jsonb_build_object('artifact_id',e.artifact_id,'sha256',e.sha256) THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,w.id,e.v30_import_parsed_jcs);
 metadata:=photo.v30_interchange_metadata_receipts(p_tenant,w.id,e.v30_import_parsed_jcs);
 IF sources IS NULL OR metadata IS NULL OR e.v30_import_evidence->'sources' IS DISTINCT FROM sources
 OR e.v30_import_evidence->'metadata' IS DISTINCT FROM metadata THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes=d.bytes AND a.bytes BETWEEN 2 AND 65536
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Params' AND artifact_id=d.artifact_id AND sha256=d.sha256)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='FrozenInterchangeImportEvidence' AND artifact_id=e.artifact_id AND sha256=e.sha256)
 THEN RETURN NULL;END IF;
 budget:=(t.execution_profile#>>'{resource_estimate,output_bytes}')::bigint;
 IF budget IS NULL OR budget NOT BETWEEN 1 AND 9007199254740991 OR coalesce(r.config_snapshot->>'snapshot_digest','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('params',jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256),
 'workflow_id',w.id,'project_id',w.project_id,'snapshot_digest',r.config_snapshot->>'snapshot_digest','max_output_bytes',budget);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_interchange_import_adoption_context(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_import_adoption_context(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_import_adoption_context(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
