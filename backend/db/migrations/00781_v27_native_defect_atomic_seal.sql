-- +goose Up
-- Atomic native CPU defect sealing and exact current-evidence receipt replay.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_native_defect_preparation(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;i photo.idempotency_records%ROWTYPE;t photo.tasks%ROWTYPE;actor uuid;session_id uuid;project uuid;ticket jsonb;next_cursor bigint;event_id uuid;admission_key text;changed integer;face jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'object' OR octet_length(p_evidence::text)>33554432 OR p_evidence->'schema_version' IS DISTINCT FROM '1'::jsonb OR (p_evidence-ARRAY['schema_version','landmarks','skin','config_text','policy','qualification','qualification_pin','params_pin','registration'])<>'{}' OR jsonb_typeof(p_evidence->'landmarks'->'admission'->'face') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 face:=p_evidence->'landmarks'->'admission'->'face';
 actor:=(face->>'actor_id')::uuid;session_id:=(face->>'session_id')::uuid;project:=(face->>'project_id')::uuid;IF face->>'operation' IS NULL OR face->>'operation' IS DISTINCT FROM 'face.landmarks' THEN RETURN NULL;END IF;
 admission_key:='defect-prep:'||p_run::text;
 IF photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS NOT TRUE THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM actor OR w.project_id IS DISTINCT FROM project OR w.workflow_type<>'semantic'
 OR face->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR (p_evidence->>'config_text')::jsonb IS DISTINCT FROM r.config_snapshot
 OR encode(public.digest(convert_to(p_evidence->>'config_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM r.config_hash
 OR encode(public.digest(convert_to(face->>'policy_text','UTF8'),'sha256'),'hex') IS DISTINCT FROM w.policy_sha THEN RETURN NULL;END IF;
 SELECT * INTO i FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=actor AND route='V2.Admit' AND idempotency_records.key=admission_key FOR UPDATE;
 IF NOT FOUND OR i.resource_id IS DISTINCT FROM p_run OR i.request_hash IS DISTINCT FROM face->>'request_sha' THEN RETURN NULL;END IF;
 ticket:=jsonb_build_object('run_id',p_run,'workflow_id',w.id,'state','queued');
 IF r.graph_sealed THEN
  IF i.response_status=202 AND i.response_body=ticket AND photo.lock_semantic_native_defect_completed_evidence(p_tenant,p_run,p_evidence) IS TRUE THEN RETURN ticket;END IF;
  RETURN NULL;
 END IF;
 IF i.response_status IS NOT NULL OR i.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 -- Native preparation currently has no consent-bearing admission contract.
 -- Reject links rather than silently bypassing generic consent epoch guards.
 IF EXISTS(SELECT 1 FROM photo.workflow_consent_links WHERE tenant_id=p_tenant AND workflow_id=w.id) THEN RETURN NULL;END IF;
 IF photo.lock_semantic_native_defect_admission_evidence(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_evidence->'registration'->>'task_id')::uuid FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version AND v2_enabled AND resource_class=t.resource_class AND backends ? 'cpu') THEN RETURN NULL;END IF;
 UPDATE photo.runs SET graph_sealed=true,status='queued',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'V27_NATIVE_SEAL_RACE' USING ERRCODE='40001';END IF;
 IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS NOT TRUE THEN RAISE EXCEPTION 'V27_NATIVE_ACTIVATION_FAILED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(p_tenant,next_cursor,p_run,actor,'principal','v2.run.queued',jsonb_build_object('run_id',p_run,'status','queued','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(p_tenant,event_id,'photo.events.v1','v2.run.queued',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.queued','tenant_id',p_tenant,'run_id',p_run,'status','queued'));
 UPDATE photo.idempotency_records SET response_status=202,response_body=ticket WHERE tenant_id=p_tenant AND id=i.id AND response_status IS NULL;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'V27_NATIVE_TICKET_RACE' USING ERRCODE='40001';END IF;
 RETURN ticket;
END $$;
ALTER FUNCTION photo.seal_semantic_native_defect_preparation(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_native_defect_preparation(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_semantic_native_defect_preparation(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
