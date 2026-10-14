-- +goose Up
-- Canonical succeeded task outputs only. Read authority is not adoption authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_export_output(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;frozen jsonb;request jsonb;rid uuid;wid uuid;tid uuid;sid uuid;project uuid;role_name text;
 r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 120
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' OR p_artifact IS NULL
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_contract IS NULL
 OR p_contract NOT IN('InterchangeResult','MappingReport','MappingShard','InterchangeArchive') THEN RETURN;END IF;
 SELECT i.response_body INTO saved FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant AND i.principal_type='user'
 AND i.principal_id=p_actor AND i.route='Interchange.Export.Pending' AND i.key=p_key AND i.request_hash=p_request_sha
 AND i.response_status IS NULL AND i.expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR saved->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR saved->>'tenant_id' IS DISTINCT FROM p_tenant::text OR saved->>'actor_id' IS DISTINCT FROM p_actor::text THEN RETURN;END IF;
 frozen:=saved->'frozen';request:=frozen->'request';project:=(request->>'project_id')::uuid;sid:=(request->>'snapshot_id')::uuid;
 rid:=photo.v30_uuid5(p_tenant,'interchange:create:'||p_actor||':'||p_key);wid:=photo.v30_uuid5(rid,'workflow');tid:=photo.v30_uuid5(wid,'v2.interchange.export:0');
 IF frozen->'schema_version' IS DISTINCT FROM '1'::jsonb OR frozen->>'request_sha256' IS DISTINCT FROM p_request_sha
 OR request->>'direction' IS DISTINCT FROM 'export' OR request->>'format' IS NULL
 OR request->>'format' NOT IN('photoeditor_bundle','xmp','captureone_sidecar') OR project IS NULL OR sid IS NULL
 OR frozen->'identity' IS DISTINCT FROM jsonb_build_object('Run',rid,'Workflow',wid,'Task',tid,'Snapshot',sid,'Params',photo.v30_uuid5(tid,'interchange:params:1'))
 OR photo.v25_lens_snapshot_ready(p_tenant,project,sid,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants tenant ON tenant.id=p.tenant_id
 WHERE p.tenant_id=p_tenant AND p.id=project AND p.archived_at IS NULL AND p.download_allowed AND tenant.status='active') THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid AND id=tid FOR SHARE;
 IF r.id IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.created_by IS DISTINCT FROM p_actor
 OR r.status NOT IN('running','completed') OR NOT r.graph_sealed OR r.cancel_requested OR r.input_snapshot_id IS DISTINCT FROM sid
 OR r.config_snapshot->>'request_sha' IS DISTINCT FROM p_request_sha OR r.config_snapshot->>'snapshot_digest' IS DISTINCT FROM ((frozen->>'plan_jcs')::jsonb->>'snapshot_digest')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.id=wid AND w.run_id=rid AND w.project_id=project
 AND w.input_snapshot_id=sid AND w.workflow_type='interchange' AND w.renderer_version='v2.2')
 OR t.id IS NULL OR t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.operation IS DISTINCT FROM 'v2.interchange.export' OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'cpu'
 OR t.params_artifact_id IS DISTINCT FROM (frozen->'params'->>'artifact_id')::uuid
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu' OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR photo.v2_input_closure_ready(tid) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=tid FOR SHARE;
 IF z.id IS NULL OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token OR z.review_required
 OR jsonb_typeof(z.outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(z.outputs) NOT BETWEEN 3 AND 53
 OR NOT z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)) THEN RETURN;END IF;
 IF p_contract='InterchangeResult' AND z.outputs->0 IS DISTINCT FROM jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha) THEN RETURN;END IF;
 role_name:=CASE p_contract WHEN 'InterchangeResult' THEN 'document' WHEN 'MappingReport' THEN 'report' WHEN 'MappingShard' THEN 'mapping_shard' ELSE 'bundle' END;
 RETURN QUERY SELECT a.bucket,a.key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.created_by_task_id=tid
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token AND a.output_role=role_name
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bytes BETWEEN 2 AND CASE WHEN p_contract='InterchangeResult' THEN 65536 WHEN p_contract='InterchangeArchive' THEN 68719476736 ELSE 8388608 END
 AND a.media_type=CASE WHEN p_contract='InterchangeArchive' THEN 'application/zip' ELSE 'application/json' END
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=tid AND i.artifact_id=a.id)
 FOR SHARE OF a;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_export_output(uuid,uuid,uuid,text,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
