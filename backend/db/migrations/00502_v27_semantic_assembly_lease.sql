-- +goose Up
-- Keep the initial graph sealed during compilation: candidate authority requires
-- a sealed graph. Publication must separately stage under this lease fence.
-- +goose StatementBegin
CREATE FUNCTION photo.open_v27_semantic_assembly(p_tenant uuid,p_run uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; parent uuid; expansion uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed
 OR w.workflow_type IS DISTINCT FROM 'semantic' OR w.renderer_version IS DISTINCT FROM 'v2.2'
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 OR jsonb_typeof(r.config_snapshot->'v27_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 IF jsonb_array_length(r.config_snapshot->'v27_semantic'->'nodes') NOT BETWEEN 1 AND 5000
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='v2.semantic.assemble')
 THEN RETURN NULL;END IF;
 parent:=(r.config_snapshot->'v27_semantic'->'nodes'->0->>'task_id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=parent
 AND operation='semantic.transfer' AND operation_version='1' AND node_key='semantic.transfer:0') THEN RETURN NULL;END IF;
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,expected_count)
 VALUES(p_tenant,p_run,parent,'v2.semantic.assemble/1','open',1)
 ON CONFLICT(tenant_id,run_id,parent_task_id,kind) DO NOTHING;
 SELECT id INTO expansion FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run
 AND parent_task_id=parent AND kind='v2.semantic.assemble/1' AND state='open';
 RETURN expansion;
EXCEPTION WHEN invalid_text_representation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.open_v27_semantic_assembly(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.open_v27_semantic_assembly(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.open_v27_semantic_assembly(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.claim_v27_semantic_assembly()
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,tenant_id uuid,run_id uuid,workflow_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE; token uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 FOR e IN SELECT x.* FROM photo.graph_expansions x JOIN photo.runs rr ON rr.tenant_id=x.tenant_id AND rr.id=x.run_id
 WHERE x.kind='v2.semantic.assemble/1' AND x.state='open' AND x.next_expansion_at<=clock_timestamp()
 AND (x.expansion_lease_until IS NULL OR x.expansion_lease_until<=clock_timestamp())
 AND rr.status='running' AND rr.graph_sealed AND NOT rr.cancel_requested
 ORDER BY x.next_expansion_at,x.created_at,x.id LOOP
  SELECT rr.* INTO r FROM photo.runs rr WHERE rr.tenant_id=e.tenant_id AND rr.id=e.run_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed THEN CONTINUE;END IF;
  SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.id=e.id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR e.state<>'open' OR e.next_expansion_at>clock_timestamp()
  OR e.expansion_lease_until>clock_timestamp() THEN CONTINUE;END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_inputs(e.tenant_id,e.run_id)) THEN CONTINUE;END IF;
  until_time:=clock_timestamp()+interval '60 seconds';
  UPDATE photo.graph_expansions SET expansion_token=token,expansion_lease_until=until_time WHERE id=e.id;
  RETURN QUERY SELECT e.id,token,until_time,e.tenant_id,e.run_id,w.id FROM photo.v2_workflows w
   WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id;
  RETURN;
 END LOOP;
END $$;
ALTER FUNCTION photo.claim_v27_semantic_assembly() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v27_semantic_assembly() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v27_semantic_assembly() TO photo_scheduler;

CREATE FUNCTION photo.renew_v27_semantic_assembly(p_expansion uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.semantic.assemble/1';
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_inputs(e.tenant_id,e.run_id)) THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET expansion_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=e.id AND tenant_id=e.tenant_id
 AND kind='v2.semantic.assemble/1' AND state='open' AND expansion_token=p_token
 AND expansion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.renew_v27_semantic_assembly(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_v27_semantic_assembly(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_v27_semantic_assembly(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.release_v27_semantic_assembly(p_expansion uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.semantic.assemble/1';
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=clock_timestamp()+interval '5 seconds' WHERE id=e.id AND tenant_id=e.tenant_id
 AND kind='v2.semantic.assemble/1' AND state='open' AND expansion_token=p_token
 AND expansion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.release_v27_semantic_assembly(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v27_semantic_assembly(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_v27_semantic_assembly(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
