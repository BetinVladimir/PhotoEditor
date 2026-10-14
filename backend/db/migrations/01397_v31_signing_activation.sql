-- +goose Up
-- Dedicated signing admission; generic graph seal cannot bypass current scope.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v31_signing;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v31_signing(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='provenance')
 OR EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='provenance.sign') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v31_signing(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v31_signing(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb,p_manifest jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ledger photo.idempotency_records%ROWTYPE;saved jsonb;rid uuid;wid uuid;tid uuid;ticket jsonb;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO ledger FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant AND x.principal_type='user' AND x.principal_id=p_actor
 AND x.route='Provenance.Sign.Pending' AND x.key=p_key AND x.request_hash=p_request_sha AND x.expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR ledger.response_body IS NULL THEN RETURN false;END IF;
 saved:=CASE WHEN ledger.response_status=202 THEN ledger.response_body->'frozen' ELSE ledger.response_body END;
 IF saved IS NULL OR saved->'task' IS DISTINCT FROM p_task THEN RETURN false;END IF;
 rid:=(saved->>'run_id')::uuid;wid:=(saved->>'workflow_id')::uuid;tid:=(saved->>'task_id')::uuid;
 ticket:=jsonb_build_object('run_id',rid,'workflow_id',wid,'state','queued');
 IF ledger.response_status IS NOT NULL THEN
  RETURN ledger.response_status=202 AND ledger.response_body->'ticket'=ticket AND ledger.resource_id=rid
  AND photo.v2_editor_authorized(p_tenant,(saved->'source_pins'->>'project')::uuid,p_actor,p_session)
  AND EXISTS(SELECT FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=tid
   WHERE r.tenant_id=p_tenant AND r.id=rid AND r.kind='v2' AND r.graph_sealed AND r.created_by=p_actor
   AND w.id=wid AND w.workflow_type='provenance' AND w.project_id=(saved->'source_pins'->>'project')::uuid
   AND t.operation='provenance.sign' AND t.operation_version='1' AND t.execution_profile->'v2_input_manifest'=p_manifest);
 END IF;
 IF photo.v31_signing_transport_ready(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT photo.v2_input_closure_ready(tid)
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)<>1
 OR EXISTS(SELECT FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=rid)
 OR NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=tid AND t.execution_profile->'v2_input_manifest'=p_manifest
  AND t.execution_profile->>'v2_entry_count'='3' AND t.execution_profile->>'v2_entries_sha'=p_task->>'input_digest') THEN RETURN false;END IF;
 -- Existing registry/qualification/resource/closure/quota/outbox gates apply.
 IF NOT photo.seal_v2_graph_pre_v31_signing(p_tenant,rid) THEN RETURN false;END IF;
 UPDATE photo.idempotency_records SET response_status=202,response_body=jsonb_build_object('schema_version',1,'frozen',saved,'ticket',ticket),resource_id=rid WHERE id=ledger.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v31_signing(uuid,uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
