-- +goose Up
-- The device pin is operator provisioned. A worker cannot attest to its own
-- device profile through this column.
ALTER TABLE photo.worker_registrations
 ADD COLUMN v23_series_device_profile_sha text
 CHECK (v23_series_device_profile_sha IS NULL OR
  v23_series_device_profile_sha ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_worker_device_matches(
 p_tenant uuid,p_worker uuid,p_profile jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.worker_registrations w
  WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker AND w.enabled
   AND p_profile->>'device_profile_sha' ~ '^[0-9a-f]{64}$'
   AND p_profile->>'runtime_lock_sha' ~ '^[0-9a-f]{64}$'
   AND w.v23_series_device_profile_sha=p_profile->>'device_profile_sha'
   AND w.lock_id=p_profile->>'runtime_lock_sha');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_worker_device_matches(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_worker_device_matches(uuid,uuid,jsonb)
 FROM PUBLIC;

-- An API pin is offered only if an active registered CPU worker matches both
-- the runtime bundle and the operator-provisioned device profile.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.v23_series_apply_qualification(uuid,uuid)'::regprocedure);
 needle:='w.lock_id=policy.runtime_lock_sha';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected series Apply resolver predicate';
 END IF;
 EXECUTE replace(definition,needle,needle||
  ' AND w.v23_series_device_profile_sha=policy.device_profile_sha');
END $$;
-- +goose StatementEnd

-- Preserve installed dispatch definitions while fencing both worker paths.
-- +goose StatementBegin
DO $$
DECLARE signature text; definition text; needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
  'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF signature LIKE '%reserve_dispatch_resources%' THEN
   needle:='AND (w.qualified_operations->t.operation) ? backend';
  ELSE
   needle:='AND (w.qualified_operations->t.operation) ? (t.execution_profile->>''backend'')';
  END IF;
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'Unexpected series dispatch worker predicate: %',signature;
  END IF;
  EXECUTE replace(definition,needle,needle||'
   AND (t.operation<>''series.balance'' OR
    t.execution_profile->>''device_profile_sha'' IS NULL OR
    photo.v23_series_worker_device_matches(t.tenant_id,w.worker_id,t.execution_profile))');
 END LOOP;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb)
 RENAME TO claim_task_pre_v23_series_device;
REVOKE ALL ON FUNCTION photo.claim_task_pre_v23_series_device(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.id IS NOT NULL AND t.operation='series.balance' AND
  t.execution_profile->>'device_profile_sha' IS NOT NULL AND
  photo.v23_series_worker_device_matches(t.tenant_id,p_worker,t.execution_profile)
   IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'unauthorized'::text,NULL::bigint,NULL::timestamptz,
   NULL::timestamptz,NULL::jsonb;
  RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.claim_task_pre_v23_series_device(
  p_task,p_attempt,p_worker,p_command);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;

ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)
 RENAME TO authorize_gateway_task_pre_v23_series_device;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task_pre_v23_series_device(uuid,integer,bigint,uuid,text)
 FROM PUBLIC,photo_gateway;
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_gateway_task(
 p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,worker_id uuid,
 attempt integer,fence bigint,lease_until timestamptz,deadline timestamptz,
 operation text,execution_profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authorized record;
BEGIN
 FOR authorized IN SELECT * FROM photo.authorize_gateway_task_pre_v23_series_device(
  p_task,p_attempt,p_fence,p_worker,p_certificate) LOOP
  IF authorized.operation='series.balance' AND
   authorized.execution_profile->>'device_profile_sha' IS NOT NULL AND
   photo.v23_series_worker_device_matches(authorized.tenant_id,p_worker,
    authorized.execution_profile) IS DISTINCT FROM true THEN
   CONTINUE;
  END IF;
  RETURN QUERY SELECT authorized.tenant_id,authorized.run_id,authorized.task_id,
   authorized.worker_id,authorized.attempt,authorized.fence,
   authorized.lease_until,authorized.deadline,authorized.operation,
   authorized.execution_profile;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)
 TO photo_gateway;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
