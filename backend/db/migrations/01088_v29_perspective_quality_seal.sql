-- +goose Up
-- The exact quality task become dispatchable in one transaction after locked rechecks.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v29_perspective_quality(p_tenant uuid,p_run uuid,p_root text,p_image text,p_original text,p_arrays jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;variant text;plan jsonb;params_sha text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 IF p_root IS NULL OR p_image IS NULL OR octet_length(p_root) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_perspective_quality_task_id IS NULL OR r.kind<>'preview' OR r.cancel_requested
 OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM r.config_snapshot->'root'->>'sha256'
 THEN RETURN false;END IF;
 IF NOT photo.v29_perspective_quality_arrays_current(p_tenant,p_run,p_root,p_image,p_original,p_arrays) THEN RETURN false;END IF;
 IF r.graph_sealed THEN RETURN r.status IN('running','completed');END IF;
 IF r.status<>'draft' THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND (state<>'blocked' OR cancel_requested))
 OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND purpose='task_params' AND state='ready')<>1
 THEN RETURN false;END IF;
 FOREACH variant IN ARRAY ARRAY['master'] LOOP
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND node_key='quality.check:'||variant;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT sha256 INTO params_sha FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
   AND owner_id=t.id AND artifact_id=t.params_artifact_id AND purpose='task_params' AND state='ready';
  IF NOT FOUND THEN RETURN false;END IF;
  plan:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
   'resource_class',t.resource_class,'timeout_seconds',600,'params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),
   'inputs',t.inputs,'input_digest',t.input_digest,'execution_profile',t.execution_profile);
  IF NOT photo.materialize_v29_perspective_quality_task(p_tenant,p_run,variant,p_root,p_image,plan) THEN RETURN false;END IF;
 END LOOP;
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_run AND state='blocked';
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v29_perspective_quality(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_perspective_quality(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v29_perspective_quality(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
