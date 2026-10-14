-- +goose Up
-- Both fixed tasks become dispatchable in one transaction after locked rechecks.
-- Draft-only transition: a sealed replay is refused rather than reactivated.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v29_multi_preview(p_tenant uuid,p_run uuid,p_root text,p_image text,p_originals jsonb,p_witnesses jsonb)
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
 IF NOT FOUND OR r.v29_multi_preview_task_id IS NULL OR r.kind<>'preview' OR r.cancel_requested
 OR encode(sha256(convert_to(p_root,'UTF8')),'hex') IS DISTINCT FROM r.config_snapshot->'root'->>'sha256'
 THEN RETURN false;END IF;
 IF r.graph_sealed OR r.status<>'draft' THEN RETURN false;END IF;
 IF photo.v29_multi_preview_originals_current(p_tenant,p_run,p_originals) IS DISTINCT FROM true
 OR photo.v29_multi_preview_witnesses_current(p_tenant,p_run,p_root,p_image,p_witnesses) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>2
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND (state<>'blocked' OR cancel_requested))
 OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND purpose='task_params' AND state='ready')<>2
 THEN RETURN false;END IF;
 FOREACH variant IN ARRAY ARRAY['preview1600','preview2560'] LOOP
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND node_key='render.derivative:'||variant;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT sha256 INTO params_sha FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
   AND owner_id=t.id AND artifact_id=t.params_artifact_id AND purpose='task_params' AND state='ready';
  IF NOT FOUND THEN RETURN false;END IF;
  plan:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
   'resource_class',t.resource_class,'timeout_seconds',300,'params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),
   'inputs',t.inputs,'input_digest',t.input_digest,'execution_profile',t.execution_profile);
  IF NOT photo.materialize_v29_multi_preview_task(p_tenant,p_run,variant,p_root,p_image,plan) THEN RETURN false;END IF;
 END LOOP;
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=p_tenant AND run_id=p_run AND state='blocked';
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v29_multi_preview(uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v29_multi_preview(uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v29_multi_preview(uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
