-- +goose Up
-- Preliminary immutable multi replacement run; no task/quota/queue admission.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_multi_replacement_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_draft text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE packet jsonb;descriptor jsonb;config jsonb;new_run uuid;workflow uuid;snapshot uuid;pin jsonb;old photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_multi_replacement_inventory(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_draft) IS DISTINCT FROM true THEN RETURN false;END IF;
 packet:=p_draft::jsonb;descriptor:=packet->'draft';new_run:=(descriptor->>'run_id')::uuid;
 workflow:=(descriptor->>'workflow_id')::uuid;snapshot:=(descriptor->>'snapshot_id')::uuid;
 pin:=descriptor->'execution_profile'->'multi_qualification';
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',(packet->>'marker_jcs')::jsonb,'v29_negative_replacement_draft_jcs',p_draft);
 SELECT * INTO old FROM photo.runs WHERE id=new_run FOR UPDATE;
 IF old.id IS NOT NULL THEN
 IF old.tenant_id IS DISTINCT FROM p_tenant OR old.created_by IS DISTINCT FROM p_actor OR old.kind IS DISTINCT FROM 'v2'
 OR old.status IS DISTINCT FROM 'draft' OR old.graph_sealed IS DISTINCT FROM false OR old.cancel_requested IS DISTINCT FROM false
 OR old.input_snapshot_id IS DISTINCT FROM snapshot OR old.config_snapshot IS DISTINCT FROM config
 OR old.config_hash IS DISTINCT FROM encode(sha256(convert_to(config::text,'UTF8')),'hex')
 OR old.failure_policy IS DISTINCT FROM 'fail_fast' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v2_workflows w WHERE w.id=workflow AND w.tenant_id=p_tenant AND w.run_id=new_run
 AND w.project_id=p_project AND w.input_snapshot_id=snapshot AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.policy_sha=pin->>'policy_sha' FOR SHARE;
 RETURN FOUND;
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(new_run,p_tenant,'v2','draft',snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(workflow,p_tenant,p_project,new_run,snapshot,'preview','v2.2',pin->>'policy_sha');
 RETURN true;
END $$;
ALTER FUNCTION photo.save_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
CREATE FUNCTION photo.read_v29_multi_replacement_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_expected text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE packet jsonb;descriptor jsonb;config jsonb;target_run uuid;workflow uuid;snapshot uuid;pin jsonb;old photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_multi_replacement_inventory(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_expected) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 packet:=p_expected::jsonb;descriptor:=packet->'draft';target_run:=(descriptor->>'run_id')::uuid;
 workflow:=(descriptor->>'workflow_id')::uuid;snapshot:=(descriptor->>'snapshot_id')::uuid;
 pin:=descriptor->'execution_profile'->'multi_qualification';
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',(packet->>'marker_jcs')::jsonb,'v29_negative_replacement_draft_jcs',p_expected);
 SELECT * INTO old FROM photo.runs r WHERE r.id=target_run FOR SHARE;
 IF old.id IS NULL OR old.tenant_id IS DISTINCT FROM p_tenant OR old.created_by IS DISTINCT FROM p_actor
 OR old.kind IS DISTINCT FROM 'v2' OR old.status IS DISTINCT FROM 'draft' OR old.graph_sealed IS DISTINCT FROM false
 OR old.cancel_requested IS DISTINCT FROM false OR old.input_snapshot_id IS DISTINCT FROM snapshot
 OR old.config_snapshot IS DISTINCT FROM config OR old.config_hash IS DISTINCT FROM encode(sha256(convert_to(config::text,'UTF8')),'hex')
 OR old.failure_policy IS DISTINCT FROM 'fail_fast' THEN RETURN NULL;END IF;
 PERFORM 1 FROM photo.v2_workflows w WHERE w.id=workflow AND w.tenant_id=p_tenant AND w.run_id=old.id
 AND w.project_id=p_project AND w.input_snapshot_id=snapshot AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.policy_sha=pin->>'policy_sha' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN old.config_snapshot->>'v29_negative_replacement_draft_jcs';
END $$;
ALTER FUNCTION photo.read_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_multi_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
