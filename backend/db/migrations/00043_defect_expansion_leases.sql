-- +goose Up
ALTER TABLE photo.graph_expansions ADD COLUMN expansion_token uuid;
ALTER TABLE photo.graph_expansions ADD COLUMN expansion_lease_until timestamptz;
ALTER TABLE photo.graph_expansions ADD COLUMN next_expansion_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE photo.graph_expansions ADD CONSTRAINT expansion_lease_pair CHECK((expansion_token IS NULL)=(expansion_lease_until IS NULL));
CREATE INDEX defect_expansion_pending ON photo.graph_expansions(next_expansion_at,created_at,id) WHERE state='open' AND kind LIKE 'review.defects/%';

-- +goose StatementBegin
CREATE FUNCTION photo.claim_defect_expansion()
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,tenant_id uuid,run_id uuid,
 review_id uuid,parent_task_id uuid,decision_hash text,route text,selection jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; e photo.graph_expansions%ROWTYPE; v photo.reviews%ROWTYPE;
 r photo.runs%ROWTYPE; fence uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT x.* INTO hint FROM photo.graph_expansions x
 JOIN photo.runs rr ON rr.id=x.run_id AND rr.tenant_id=x.tenant_id
 JOIN photo.reviews vv ON vv.tenant_id=x.tenant_id AND vv.run_id=x.run_id AND x.kind='review.defects/'||vv.id::text AND vv.task_id=x.parent_task_id
 WHERE x.state='open' AND x.next_expansion_at<=clock_timestamp()
 AND (x.expansion_lease_until IS NULL OR x.expansion_lease_until<=clock_timestamp())
 AND NOT rr.cancel_requested AND NOT rr.graph_sealed AND rr.status IN ('running','waiting_review')
 AND vv.state='decided' AND vv.decision_hash IS NOT NULL AND vv.decision_route IN ('repair','base','skip')
 ORDER BY x.next_expansion_at,x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT rr.* INTO r FROM photo.runs rr WHERE rr.id=hint.run_id AND rr.tenant_id=hint.tenant_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR r.cancel_requested OR r.graph_sealed OR r.status NOT IN ('running','waiting_review') THEN RETURN;END IF;
 SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.id=hint.id AND x.tenant_id=r.tenant_id AND x.run_id=r.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR e.state<>'open' OR e.next_expansion_at>clock_timestamp() OR (e.expansion_lease_until IS NOT NULL AND e.expansion_lease_until>clock_timestamp()) THEN RETURN;END IF;
 SELECT vv.* INTO v FROM photo.reviews vv WHERE vv.tenant_id=e.tenant_id AND vv.run_id=e.run_id
 AND e.kind='review.defects/'||vv.id::text AND vv.task_id=e.parent_task_id AND vv.kind='defects' AND vv.state='decided';
 IF NOT FOUND OR v.decision_hash IS NULL OR v.decision_route NOT IN ('repair','base','skip') OR jsonb_typeof(v.selection) IS DISTINCT FROM 'object'
 OR NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=e.tenant_id AND t.run_id=e.run_id AND t.id=e.parent_task_id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL AND t.operation='defects.propose') THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.graph_expansions SET expansion_token=fence,expansion_lease_until=until_time WHERE id=e.id;
 RETURN QUERY SELECT e.id,fence,until_time,e.tenant_id,e.run_id,v.id,e.parent_task_id,v.decision_hash,v.decision_route,v.selection;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_defect_expansion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_defect_expansion() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_defect_expansion() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_defect_expansion(p_expansion uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; r photo.runs%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 SELECT * INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.graph_sealed OR r.status NOT IN ('running','waiting_review') THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET expansion_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE id=p_expansion AND tenant_id=r.tenant_id AND run_id=r.id AND state='open' AND kind LIKE 'review.defects/%'
 AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_defect_expansion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_defect_expansion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_defect_expansion(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_defect_expansion(p_expansion uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 SELECT * INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 UPDATE photo.graph_expansions SET expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=clock_timestamp()+interval '5 seconds'
 WHERE id=p_expansion AND tenant_id=hint.tenant_id AND run_id=hint.run_id AND state='open' AND kind LIKE 'review.defects/%' AND expansion_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_defect_expansion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_defect_expansion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_defect_expansion(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
