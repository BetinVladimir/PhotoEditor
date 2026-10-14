-- +goose Up
-- Temporary unseal remains inside each locked function call; no task activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_transport_scope(p_tenant uuid,p_run uuid,p_task uuid,p_parsed text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='interchange';
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF w.id IS NULL OR r.status<>'running' OR NOT r.graph_sealed OR r.cancel_requested
 OR t.id IS NULL OR t.id<>photo.v30_uuid5(w.id,'v2.interchange.import:0') OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation<>'v2.interchange.import' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.params_artifact_id<>photo.v30_uuid5(t.id,'interchange:params:1') THEN RETURN NULL;END IF;
 IF photo.v30_interchange_import_sources(p_tenant,w.id,p_parsed) IS NULL
 OR photo.v30_interchange_metadata_receipts(p_tenant,w.id,p_parsed) IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
  AND state='reserved' AND expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN w.id;
END $$;
ALTER FUNCTION photo.v30_interchange_transport_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_transport_scope(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.register_v30_interchange_input_batch(p_tenant uuid,p_run uuid,p_task uuid,p_first integer,p_rows jsonb,p_parsed text) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid;result integer;t photo.tasks%ROWTYPE;expected jsonb;
BEGIN
 workflow:=photo.v30_interchange_transport_scope(p_tenant,p_run,p_task,p_parsed);
 IF workflow IS NULL THEN RAISE EXCEPTION 'INTERCHANGE_TRANSPORT_STALE' USING ERRCODE='23514';END IF;
 IF p_first IS NULL OR p_first NOT BETWEEN 0 AND 999999 OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_rows) NOT BETWEEN 1 AND 512 OR pg_column_size(p_rows)>1048576
 OR p_first+jsonb_array_length(p_rows)>1000000 THEN
  RAISE EXCEPTION 'INTERCHANGE_INPUT_BATCH_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.execution_profile ? 'v2_input_manifest' THEN
  SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.ordinal)
   INTO expected FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal>=p_first AND i.ordinal<p_first+jsonb_array_length(p_rows);
  IF expected IS DISTINCT FROM p_rows OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RAISE EXCEPTION 'INTERCHANGE_INPUT_REPLAY_CONFLICT' USING ERRCODE='23514';END IF;
  RETURN jsonb_array_length(p_rows);
 END IF;
 UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=p_tenant AND id=p_run;
 result:=photo.register_v2_task_input_batch(p_tenant,p_run,p_task,p_first,p_rows);
 UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=p_tenant AND id=p_run;
 IF photo.v30_interchange_transport_scope(p_tenant,p_run,p_task,p_parsed) IS DISTINCT FROM workflow THEN
  RAISE EXCEPTION 'INTERCHANGE_TRANSPORT_FENCE_CHANGED' USING ERRCODE='40001';END IF;
 RETURN result;
END $$;
ALTER FUNCTION photo.register_v30_interchange_input_batch(uuid,uuid,uuid,integer,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v30_interchange_input_batch(uuid,uuid,uuid,integer,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.register_v30_interchange_input_batch(uuid,uuid,uuid,integer,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v30_interchange_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,
 p_count integer,p_sha text,p_roots jsonb,p_parsed text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow uuid;result boolean;t photo.tasks%ROWTYPE;chunks jsonb;
BEGIN
 workflow:=photo.v30_interchange_transport_scope(p_tenant,p_run,p_task,p_parsed);
 IF workflow IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.execution_profile ? 'v2_input_manifest' THEN
  SELECT jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal) INTO chunks
   FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='InputClosureChunk';
  RETURN t.execution_profile->'v2_input_manifest'=p_manifest AND (t.execution_profile->>'v2_entry_count')::integer=p_count
   AND t.execution_profile->>'v2_entries_sha'=p_sha AND t.inputs=jsonb_build_array(p_manifest)||p_roots AND chunks=p_chunks
   AND photo.v2_input_closure_ready(p_task);
 END IF;
 UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=p_tenant AND id=p_run;
 result:=photo.seal_v2_task_inputs(p_tenant,p_run,p_task,p_manifest,p_chunks,p_count,p_sha,p_roots);
 UPDATE photo.runs SET graph_sealed=true WHERE tenant_id=p_tenant AND id=p_run;
 IF result IS DISTINCT FROM true OR photo.v30_interchange_transport_scope(p_tenant,p_run,p_task,p_parsed) IS DISTINCT FROM workflow THEN
  RAISE EXCEPTION 'INTERCHANGE_TRANSPORT_SEAL_REFUSED' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.seal_v30_interchange_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
