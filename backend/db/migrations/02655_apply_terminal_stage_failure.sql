-- Forward-only failure propagation for sealed Apply v4 required stages.
-- Never resets/replays tasks or changes source data, output authority or billing.
-- +goose Up
-- +goose StatementBegin
DO $guard$ BEGIN
 IF to_regprocedure('photo.apply_required_stage_failed(uuid,uuid)') IS NOT NULL
 OR to_regprocedure('photo.fail_apply_terminal_stage(uuid,uuid,uuid)') IS NOT NULL THEN
 RAISE EXCEPTION 'APPLY_FAILURE_NAME_COLLISION' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.claim_apply_build_versioned()'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '9e1bb31419af1892f7d4ca54150199f5e6b23c263ee38332d903b3b025654248' THEN RAISE EXCEPTION 'APPLY_FAILURE_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.fail_apply_build(uuid,uuid,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '45273c5965c3495ddb2d72fddba833f0c14a7bed6ed2e2bfa5df3731ab0bec2d' THEN RAISE EXCEPTION 'APPLY_FAILURE_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.fail_apply_build_before_billing(uuid,uuid,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '83c38199db0e3295dee8330432d2a808ee91200bcf95a1192464f2657200e2d6' THEN RAISE EXCEPTION 'APPLY_FAILURE_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.pending_task_failures(integer)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '7d65631f84359f8d0eecdd400c95f8fb2061cd9375031aba87a8d17e8d28b0b4' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamp with time zone,jsonb,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '245c8ed214214afce7bba7a67211f78ea125c56746de3234d34399651e71bd50' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.reap_tasks(integer)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e88ca000975946b9f30c0f0655b474c6942268147f890f6745b42481b4e876a9' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
END $guard$;
CREATE TEMP TABLE apply_failure_2655_metadata ON COMMIT DROP AS SELECT p.oid,to_jsonb(p)-'prosrc' AS body FROM pg_proc p WHERE p.oid IN('photo.claim_apply_build_versioned()'::regprocedure,'photo.fail_apply_build(uuid,uuid,text)'::regprocedure,'photo.fail_apply_build_before_billing(uuid,uuid,text)'::regprocedure,'photo.pending_task_failures(integer)'::regprocedure,'photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamp with time zone,jsonb,text)'::regprocedure,'photo.reap_tasks(integer)'::regprocedure);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.apply_required_stage_failed(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $body$
 SELECT EXISTS(
 SELECT FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.apply_v4_stages s ON s.tenant_id=b.tenant_id AND s.run_id=b.run_id
  AND ((b.phase='adopt' AND s.phase='quality' AND s.stage_index=b.stage_index)
   OR (b.phase<>'adopt' AND s.stage_index=b.stage_index-1))
 JOIN photo.apply_nodes n ON n.tenant_id=s.tenant_id AND n.run_id=s.run_id AND n.phase=s.phase AND n.operation=s.operation
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id AND t.operation=n.operation
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.graph_version=4 AND b.phase NOT IN('complete','failed')
 AND r.kind='apply' AND r.status IN('queued','running') AND NOT r.cancel_requested
 AND s.cardinality IN('item','aggregate','group') AND s.operation IS NOT NULL
 AND ((b.phase='adopt' AND NOT EXISTS(SELECT FROM photo.apply_v4_stages next_stage WHERE next_stage.tenant_id=b.tenant_id AND next_stage.run_id=b.run_id AND next_stage.stage_index>b.stage_index))
 OR(b.phase<>'adopt' AND EXISTS(SELECT FROM photo.apply_v4_stages current_stage WHERE current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index AND current_stage.phase=b.phase)))
 AND NOT EXISTS(SELECT FROM photo.apply_nodes x WHERE x.tenant_id=s.tenant_id AND x.run_id=s.run_id AND x.phase=s.phase AND x.operation IS DISTINCT FROM s.operation)
 AND (SELECT count(*) FROM photo.apply_nodes x JOIN photo.tasks xt ON xt.tenant_id=x.tenant_id AND xt.run_id=x.run_id AND xt.id=x.id AND xt.operation=x.operation WHERE x.tenant_id=s.tenant_id AND x.run_id=s.run_id AND x.phase=s.phase)=
  CASE s.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN s.expected_nodes ELSE -1 END

 AND (SELECT count(*) FROM photo.apply_nodes x WHERE x.tenant_id=s.tenant_id AND x.run_id=s.run_id AND x.phase=s.phase)=
  CASE s.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN s.expected_nodes ELSE -1 END
 AND t.canonical_result_id IS NULL AND t.next_attempt_at IS NULL
 AND (t.state='cancelled' OR (t.state='failed' AND EXISTS(SELECT FROM photo.task_attempts a WHERE a.tenant_id=t.tenant_id AND a.task_id=t.id
  AND a.attempt=t.attempt AND a.worker_id=t.worker_id AND a.finished_at IS NOT NULL AND jsonb_typeof(a.error)='object' AND jsonb_typeof(a.error->'code')='string'
 AND (a.fencing_token=t.fencing_token OR (a.fencing_token=t.fencing_token-1 AND a.worker_id=t.worker_id
  AND a.error->>'code' IN('TIMEOUT','LEASE_LOST') AND a.error->'retryable'='true'::jsonb
  AND a.error->'args'='{}'::jsonb AND t.terminal_reason=a.error->>'code'
  AND a.error->>'message_key'=CASE a.error->>'code' WHEN 'TIMEOUT' THEN 'errors.timeout' ELSE 'errors.lease_lost' END))))))
$body$;
ALTER FUNCTION photo.apply_required_stage_failed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_required_stage_failed(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- Failure claim retains its sealed graph. Normal successful claims keep their
-- exact former opening/claim behavior, so partial materializations never become
-- a false failure authority.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_apply_build_versioned()
 RETURNS TABLE(tenant_id uuid, run_id uuid, source_collection_id uuid, style_version_id uuid, phase text, item_count integer, lease_token uuid, lease_until timestamp with time zone, graph_version integer, stage_index integer)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE legacy record;
BEGIN
 SELECT * INTO legacy FROM photo.claim_apply_build();
 IF FOUND THEN RETURN QUERY SELECT legacy.tenant_id,legacy.run_id,legacy.source_collection_id,legacy.style_version_id,legacy.phase,legacy.item_count,legacy.lease_token,legacy.lease_until,b.graph_version,b.stage_index FROM photo.apply_builds b WHERE b.tenant_id=legacy.tenant_id AND b.run_id=legacy.run_id AND b.lease_token=legacy.lease_token;RETURN;END IF;
 RETURN QUERY WITH candidate AS (
  SELECT b.tenant_id,b.run_id FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
  LEFT JOIN photo.apply_v4_stages current_stage ON current_stage.tenant_id=b.tenant_id AND current_stage.run_id=b.run_id AND current_stage.stage_index=b.stage_index
  LEFT JOIN photo.apply_v4_stages previous_stage ON previous_stage.tenant_id=b.tenant_id AND previous_stage.run_id=b.run_id AND previous_stage.stage_index=b.stage_index-1
  WHERE b.graph_version=4 AND b.phase NOT IN('complete','failed') AND (b.lease_until IS NULL OR b.lease_until<clock_timestamp()) AND NOT r.cancel_requested
   AND ((r.graph_sealed AND photo.apply_required_stage_failed(b.tenant_id,b.run_id)) OR (b.phase='adopt' AND r.status IN('queued','running') AND r.graph_sealed AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='quality')=b.item_count)
    OR (b.stage_index>0 AND current_stage.cardinality<>'review' AND r.status IN('queued','running') AND r.graph_sealed AND
       ((previous_stage.cardinality IN('item','aggregate','group') AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN previous_stage.expected_nodes ELSE -1 END)
        OR (previous_stage.phase='cull_review' AND EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id AND cv.state='decided' AND cv.decision_route='selection')) OR (previous_stage.phase='defects_review' AND (SELECT count(*) FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.state='decided' AND (v.decision_route<>'repair' OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL)))=b.item_count)
        OR (previous_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND (SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=(SELECT q.phase FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2) AND v.state='decided')=b.item_count)
        OR (previous_stage.phase='dust_review' AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id LEFT JOIN photo.reviews v ON v.tenant_id=n.tenant_id AND v.task_id=n.id AND v.kind='sensor_dust' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=(SELECT q.phase FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2) AND t.state='succeeded' AND (NOT z.review_required OR v.state='decided'))=(SELECT q.expected_nodes FROM photo.apply_v4_stages q WHERE q.tenant_id=b.tenant_id AND q.run_id=b.run_id AND q.stage_index=b.stage_index-2))))
    OR (b.stage_index>0 AND current_stage.cardinality='review' AND r.status IN('running','waiting_review') AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase)=CASE previous_stage.cardinality WHEN 'item' THEN b.item_count WHEN 'aggregate' THEN 1 WHEN 'group' THEN previous_stage.expected_nodes ELSE -1 END
       AND ((current_stage.phase='cull_review' AND (NOT EXISTS(SELECT FROM photo.apply_culling_reviews cr WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id) OR EXISTS(SELECT FROM photo.apply_culling_reviews cr JOIN photo.reviews cv ON cv.tenant_id=cr.tenant_id AND cv.id=cr.review_id WHERE cr.tenant_id=b.tenant_id AND cr.run_id=b.run_id AND cv.state='decided' AND cv.decision_route='selection'))) OR current_stage.phase='defects_review' AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='defects' AND n.phase=previous_stage.phase)<b.item_count OR ((SELECT count(*) FROM photo.defect_branches d WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id)=b.item_count AND (EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=b.tenant_id AND e.run_id=b.run_id AND e.state='materialized') OR NOT EXISTS(SELECT FROM photo.defect_branches d JOIN photo.reviews v ON v.tenant_id=d.tenant_id AND v.id=d.review_id WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND v.decision_route='repair' AND EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=d.tenant_id AND t.id=d.task_id AND t.state<>'succeeded'))))) OR current_stage.phase IN('background_review','inpaint_review','generation_review','composition_review') AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase)<b.item_count OR NOT EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND n.phase=previous_stage.phase AND v.state<>'decided'))
        OR current_stage.phase='dust_review' AND previous_stage.cardinality='group' AND (SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase AND t.state='succeeded')=previous_stage.expected_nodes AND ((SELECT count(*) FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='sensor_dust' AND n.phase=previous_stage.phase)<(SELECT count(*) FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase=previous_stage.phase AND z.review_required) OR NOT EXISTS(SELECT FROM photo.reviews v JOIN photo.apply_nodes n ON n.tenant_id=v.tenant_id AND n.id=v.task_id WHERE v.tenant_id=b.tenant_id AND v.run_id=b.run_id AND v.kind='sensor_dust' AND n.phase=previous_stage.phase AND v.state<>'decided')))))
  ORDER BY b.updated_at,b.run_id FOR UPDATE OF b SKIP LOCKED LIMIT 1
 ),opened AS (UPDATE photo.runs r SET graph_sealed=false,status=CASE WHEN r.status='waiting_review' THEN 'running' ELSE r.status END,revision=revision+1 FROM candidate c JOIN photo.apply_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id WHERE r.tenant_id=c.tenant_id AND r.id=c.run_id AND r.graph_sealed AND b.phase<>'adopt' AND NOT photo.apply_required_stage_failed(b.tenant_id,b.run_id) RETURNING r.id),
 claimed AS (UPDATE photo.apply_builds b SET lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() FROM candidate c WHERE b.tenant_id=c.tenant_id AND b.run_id=c.run_id AND (photo.apply_required_stage_failed(b.tenant_id,b.run_id) OR b.phase='adopt' OR EXISTS(SELECT FROM opened o WHERE o.id=b.run_id) OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=b.tenant_id AND r.id=b.run_id AND NOT r.graph_sealed)) RETURNING b.*)
 SELECT c.tenant_id,c.run_id,c.source_collection_id,c.style_version_id,c.phase,c.item_count,c.lease_token,c.lease_until,c.graph_version,c.stage_index FROM claimed c;
END $function$
;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.fail_apply_terminal_stage(p_tenant uuid,p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $body$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_token IS NULL THEN RETURN false;END IF;
 -- Preserve the existing public failure wrapper's billing -> build -> run
 -- lock order. Reacquiring its billing locks below is intentional/reentrant.
 PERFORM photo.billing_lock_apply_run(p_tenant,p_run);
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.graph_version<>4 OR b.phase IN('complete','failed') OR b.lease_token IS DISTINCT FROM p_token
 OR b.lease_until IS NULL OR b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'apply' OR r.status NOT IN('queued','running') OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 -- Terminal task rows are locked only after their parent run, not in the
 -- speculative global claim predicate. No relaxed task lock or RLS contract.
 PERFORM t.id FROM photo.apply_v4_stages s JOIN photo.apply_nodes n ON n.tenant_id=s.tenant_id AND n.run_id=s.run_id AND n.phase=s.phase
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
 WHERE s.tenant_id=p_tenant AND s.run_id=p_run AND ((b.phase='adopt' AND s.phase='quality' AND s.stage_index=b.stage_index)
 OR(b.phase<>'adopt' AND s.stage_index=b.stage_index-1)) ORDER BY t.id FOR SHARE OF t;
 IF b.lease_until<clock_timestamp() OR NOT photo.apply_required_stage_failed(p_tenant,p_run) THEN RETURN false;END IF;
 -- Retain quota consistency/release, run revision, billing settlement, current
 -- cancellation and lease checks in the existing PUBLIC production primitive.
 RETURN photo.fail_apply_build(p_run,p_token,'APPLY_STAGE_FAILED');
END $body$;
ALTER FUNCTION photo.fail_apply_terminal_stage(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_apply_terminal_stage(uuid,uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- Strip ambient default EXECUTE only from the two new functions; then grant
-- just the public lease-bound failure primitive to the scheduler.
-- +goose StatementBegin
DO $acl$ DECLARE f regprocedure;g record;BEGIN
 FOREACH f IN ARRAY ARRAY['photo.apply_required_stage_failed(uuid,uuid)'::regprocedure,'photo.fail_apply_terminal_stage(uuid,uuid,uuid)'::regprocedure] LOOP
  FOR g IN SELECT DISTINCT a.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=f AND a.grantee<>p.proowner LOOP
   IF g.grantee=0 THEN EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',f);
   ELSE EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f,pg_get_userbyid(g.grantee));END IF;
  END LOOP;
 END LOOP;
END $acl$;
GRANT EXECUTE ON FUNCTION photo.fail_apply_terminal_stage(uuid,uuid,uuid) TO photo_scheduler;
DO $post$ BEGIN
 IF EXISTS(SELECT FROM pg_proc p WHERE p.oid IN('photo.apply_required_stage_failed(uuid,uuid)'::regprocedure,'photo.fail_apply_terminal_stage(uuid,uuid,uuid)'::regprocedure) AND (pg_get_userbyid(p.proowner)<>'photo_fn_owner' OR NOT p.prosecdef OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo']::text[])) THEN RAISE EXCEPTION 'APPLY_FAILURE_NEW_AUTHORITY' USING ERRCODE='55000';END IF;
 IF EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid IN('photo.apply_required_stage_failed(uuid,uuid)'::regprocedure,'photo.fail_apply_terminal_stage(uuid,uuid,uuid)'::regprocedure) AND (a.privilege_type<>'EXECUTE' OR a.is_grantable OR (a.grantee<>p.proowner AND NOT(p.oid='photo.fail_apply_terminal_stage(uuid,uuid,uuid)'::regprocedure AND a.grantee=(SELECT oid FROM pg_roles WHERE rolname='photo_scheduler'))))) THEN RAISE EXCEPTION 'APPLY_FAILURE_NEW_ACL' USING ERRCODE='55000';END IF;
 IF EXISTS(SELECT FROM apply_failure_2655_metadata m JOIN pg_proc p ON p.oid=m.oid WHERE (to_jsonb(p)-'prosrc') IS DISTINCT FROM m.body)
 OR (SELECT count(*) FROM apply_failure_2655_metadata)<>6 THEN RAISE EXCEPTION 'APPLY_FAILURE_METADATA_CHANGED' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.fail_apply_build(uuid,uuid,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '45273c5965c3495ddb2d72fddba833f0c14a7bed6ed2e2bfa5df3731ab0bec2d' THEN RAISE EXCEPTION 'APPLY_FAILURE_WRAPPER_CHANGED' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.fail_apply_build_before_billing(uuid,uuid,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '83c38199db0e3295dee8330432d2a808ee91200bcf95a1192464f2657200e2d6' THEN RAISE EXCEPTION 'APPLY_FAILURE_WRAPPER_CHANGED' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.pending_task_failures(integer)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '7d65631f84359f8d0eecdd400c95f8fb2061cd9375031aba87a8d17e8d28b0b4' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamp with time zone,jsonb,text)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM '245c8ed214214afce7bba7a67211f78ea125c56746de3234d34399651e71bd50' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
 IF encode(sha256(convert_to(pg_get_functiondef('photo.reap_tasks(integer)'::regprocedure),'UTF8')),'hex') IS DISTINCT FROM 'e88ca000975946b9f30c0f0655b474c6942268147f890f6745b42481b4e876a9' THEN RAISE EXCEPTION 'APPLY_FAILURE_REAPER_PREIMAGE' USING ERRCODE='55000';END IF;
END $post$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
