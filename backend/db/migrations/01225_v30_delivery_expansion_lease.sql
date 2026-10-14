-- +goose Up
CREATE INDEX v30_delivery_expansion_pending ON photo.graph_expansions(next_expansion_at,created_at,id)
 WHERE state='open' AND kind='v2.delivery/1';
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_expansion_ready(p_expansion uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;b photo.backend_documents%ROWTYPE;
 input_ref jsonb;params text;frozen jsonb;snapshot jsonb;slots integer;
BEGIN
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND OR e.state<>'open' OR coalesce(e.cursor,'') !~ '^(0|[1-9][0-9]{0,4})$' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id=e.parent_task_id;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=e.tenant_id AND workflow_id=w.id;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested
 OR t.id IS NULL OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS NULL
 OR t.operation<>'v2.delivery.prepare' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR w.id IS NULL OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2'
 OR d.id IS NULL OR d.state<>'building' OR d.project_id<>w.project_id OR d.snapshot_id<>w.input_snapshot_id
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=e.tenant_id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
  AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id
 AND owner_id=t.id AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF b.id IS NULL OR b.v30_delivery_proof IS NULL THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id AND contract='FrozenDeliveryInput')<>1 THEN RETURN false;END IF;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO input_ref FROM photo.v2_task_inputs
 WHERE tenant_id=e.tenant_id AND task_id=t.id AND contract='FrozenDeliveryInput';
 -- The private source predicate consumes the Params semantics; original Params
 -- SHA/bytes were bound at admission and adoption, not recomputed from jsonb text.
 params:=jsonb_build_object('schema_version',1,'input',input_ref)::text;
 IF photo.v30_delivery_prepare_source_ready(e.tenant_id,e.run_id,t.id,t.params_artifact_id,b.sha256,params) IS DISTINCT FROM true THEN RETURN false;END IF;
 frozen:=(b.v30_delivery_proof->>'frozen_input_jcs')::jsonb;
 snapshot:=(b.v30_delivery_proof->>'snapshot_jcs')::jsonb;
 slots:=jsonb_array_length(snapshot->'items')*jsonb_array_length(frozen->'presets');
 IF slots NOT BETWEEN 1 AND 80000 OR e.expected_count<>slots OR e.cursor::integer>slots
 OR (SELECT count(*) FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id)<>slots THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_expansion_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_ready(uuid) FROM PUBLIC,photo_scheduler,photo_api,photo_worker;

CREATE FUNCTION photo.claim_v30_delivery_expansion()
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,tenant_id uuid,run_id uuid,workflow_id uuid,package_id uuid,parent_task_id uuid,slot_cursor integer,slot_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE;e photo.graph_expansions%ROWTYPE;token uuid;until_time timestamptz;
BEGIN
 FOR hint IN SELECT x.* FROM photo.graph_expansions x JOIN photo.runs r ON r.tenant_id=x.tenant_id AND r.id=x.run_id
 WHERE x.kind='v2.delivery/1' AND x.state='open' AND x.next_expansion_at<=clock_timestamp()
 AND (x.expansion_lease_until IS NULL OR x.expansion_lease_until<=clock_timestamp())
 AND r.status='running' AND NOT r.graph_sealed AND NOT r.cancel_requested
 ORDER BY x.next_expansion_at,x.created_at,x.id LOOP
  PERFORM id FROM photo.tenants WHERE id=hint.tenant_id AND status='active' FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN CONTINUE;END IF;
  PERFORM id FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN CONTINUE;END IF;
  SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.id=hint.id AND x.tenant_id=hint.tenant_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR e.state<>'open' OR e.kind<>'v2.delivery/1' OR e.next_expansion_at>clock_timestamp()
  OR e.expansion_lease_until>clock_timestamp() OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN CONTINUE;END IF;
  token:=gen_random_uuid();until_time:=clock_timestamp()+interval '60 seconds';
  UPDATE photo.graph_expansions SET expansion_token=token,expansion_lease_until=until_time WHERE id=e.id;
  RETURN QUERY SELECT e.id,token,until_time,e.tenant_id,e.run_id,w.id,d.id,e.parent_task_id,e.cursor::integer,e.expected_count::integer
  FROM photo.v2_workflows w JOIN photo.delivery_packages d ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id
  WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id;
  RETURN;
 END LOOP;
END $$;
ALTER FUNCTION photo.claim_v30_delivery_expansion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_delivery_expansion() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v30_delivery_expansion() TO photo_scheduler;

CREATE FUNCTION photo.update_v30_delivery_expansion_lease(p_expansion uuid,p_token uuid,p_release boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_release IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.id=p_expansion AND x.tenant_id=e.tenant_id FOR UPDATE;
 IF NOT FOUND OR e.state<>'open' OR e.kind<>'v2.delivery/1' OR e.expansion_token IS DISTINCT FROM p_token
 OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF NOT p_release AND photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET
 expansion_token=CASE WHEN p_release THEN NULL ELSE p_token END,
 expansion_lease_until=CASE WHEN p_release THEN NULL ELSE clock_timestamp()+interval '60 seconds' END,
 next_expansion_at=CASE WHEN p_release THEN clock_timestamp()+interval '5 seconds' ELSE next_expansion_at END
 WHERE id=e.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.update_v30_delivery_expansion_lease(uuid,uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.update_v30_delivery_expansion_lease(uuid,uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.update_v30_delivery_expansion_lease(uuid,uuid,boolean) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
