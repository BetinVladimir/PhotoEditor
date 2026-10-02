-- Claim only a session barrier whose descriptor outputs are all canonical and
-- adopted. The token fences concurrent compiler instances and is renewable.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v22_session_plan()
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,
 tenant_id uuid,run_id uuid,workflow_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 r photo.runs%ROWTYPE; v_count integer; v_ready integer;
 fence uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT x.* INTO hint FROM photo.graph_expansions x
 JOIN photo.runs rr ON rr.tenant_id=x.tenant_id AND rr.id=x.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=x.tenant_id AND w.run_id=x.run_id
 JOIN photo.tenants tenant ON tenant.id=x.tenant_id
 WHERE x.kind='v2.session.plan/1' AND x.state='open'
  AND x.next_expansion_at<=clock_timestamp()
  AND (x.expansion_lease_until IS NULL OR x.expansion_lease_until<=clock_timestamp())
  AND rr.kind='v2' AND rr.status='running' AND NOT rr.cancel_requested
  AND NOT rr.graph_sealed AND w.workflow_type='session'
  AND w.renderer_version='v2.2' AND tenant.status='active'
 ORDER BY x.next_expansion_at,x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT rr.* INTO r FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id
  AND rr.id=hint.run_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR r.cancel_requested OR r.status<>'running' OR r.graph_sealed THEN RETURN; END IF;
 SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.tenant_id=hint.tenant_id
  AND x.id=hint.id AND x.run_id=r.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR e.kind<>'v2.session.plan/1' OR e.state<>'open'
  OR e.next_expansion_at>clock_timestamp() OR
  (e.expansion_lease_until IS NOT NULL AND e.expansion_lease_until>clock_timestamp()) THEN RETURN; END IF;
 SELECT count(*) INTO v_count FROM photo.tasks t WHERE t.tenant_id=e.tenant_id
  AND t.run_id=e.run_id AND t.operation='session.describe';
 SELECT count(*) INTO v_ready FROM photo.v22_session_plan_inputs(e.tenant_id,e.run_id);
 IF v_count NOT BETWEEN 1 AND 40 OR v_ready<>v_count THEN RETURN; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.graph_expansions x SET expansion_token=fence,expansion_lease_until=until_time
  WHERE x.tenant_id=e.tenant_id AND x.id=e.id;
 RETURN QUERY SELECT e.id,fence,until_time,e.tenant_id,e.run_id,w.id
  FROM photo.v2_workflows w WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_v22_session_plan() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v22_session_plan() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v22_session_plan() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_v22_session_plan(p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false; END IF;
 SELECT * INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND OR hint.kind<>'v2.session.plan/1' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=hint.tenant_id
  AND id=hint.run_id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status<>'running' OR r.graph_sealed THEN RETURN false; END IF;
 UPDATE photo.graph_expansions SET expansion_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE tenant_id=hint.tenant_id AND id=hint.id AND run_id=hint.run_id
  AND kind='v2.session.plan/1' AND state='open'
  AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_v22_session_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_v22_session_plan(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_v22_session_plan(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_v22_session_plan(p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false; END IF;
 SELECT * INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND OR hint.kind<>'v2.session.plan/1' THEN RETURN false; END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 UPDATE photo.graph_expansions SET expansion_token=NULL,expansion_lease_until=NULL,
  next_expansion_at=clock_timestamp()+interval '5 seconds'
 WHERE tenant_id=hint.tenant_id AND id=hint.id AND run_id=hint.run_id
  AND kind='v2.session.plan/1' AND state='open' AND expansion_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_v22_session_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v22_session_plan(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_v22_session_plan(uuid,uuid) TO photo_scheduler;
