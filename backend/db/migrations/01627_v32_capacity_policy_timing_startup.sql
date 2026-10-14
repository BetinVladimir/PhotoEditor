-- +goose Up
-- Authenticated read-only startup proof. No registration or installation grants.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_policy_timing_ready(p_worker uuid,p_sha text,p_offline bigint,p_lease bigint)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.capacity_policy_timings c WHERE c.policy_sha=p_sha
  AND c.offline_seconds=p_offline AND c.lease_seconds=p_lease)
 AND EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker
  AND w.db_principal=session_user AND w.enabled AND w.capacity_policy_sha=p_sha)
 AND NOT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.worker_id=p_worker
  AND w.db_principal=session_user AND w.enabled AND w.capacity_policy_sha IS DISTINCT FROM p_sha);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_policy_timing_ready(uuid,text,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_policy_timing_ready(uuid,text,bigint,bigint) FROM PUBLIC,photo_api,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.capacity_policy_timing_ready(uuid,text,bigint,bigint) TO photo_worker;
-- A missing/revoked operator policy must stop capacity reporting as well as admission.
-- Lock timing before node/registration locks; its contents cannot be updated.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 needle text:=' SELECT count(DISTINCT w.node_id),min(w.node_id) INTO nodes,node FROM photo.worker_registrations w';
 gate text:=' PERFORM policy_sha FROM photo.capacity_policy_timings WHERE policy_sha=p_policy_sha FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
';
BEGIN
 definition:=pg_get_functiondef('photo.heartbeat_worker_capacity_v2(uuid,text,text,text[],bigint,bigint,bigint,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected capacity timing heartbeat definition';
 END IF;
 EXECUTE replace(definition,needle,gate||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
