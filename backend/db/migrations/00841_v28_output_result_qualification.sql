-- +goose Up
-- Recheck the frozen qualification inside output/result acceptance transactions.
-- +goose StatementBegin
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) RENAME TO authorize_gateway_task_pre_v28;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task_pre_v28(uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_gateway;
CREATE FUNCTION photo.authorize_gateway_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,worker_id uuid,attempt integer,fence bigint,lease_until timestamptz,deadline timestamptz,operation text,execution_profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authorized record;
BEGIN
 FOR authorized IN SELECT * FROM photo.authorize_gateway_task_pre_v28(p_task,p_attempt,p_fence,p_worker,p_certificate) LOOP
  IF authorized.operation IN('portrait.hair','portrait.clothing')
   AND photo.v28_local_qualification_pin_ready(authorized.tenant_id,authorized.run_id,authorized.task_id) IS DISTINCT FROM true THEN CONTINUE;END IF;
  RETURN QUERY SELECT authorized.tenant_id,authorized.run_id,authorized.task_id,authorized.worker_id,authorized.attempt,authorized.fence,
   authorized.lease_until,authorized.deadline,authorized.operation,authorized.execution_profile;
 END LOOP;
END $$;
ALTER FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_gateway_task(uuid,integer,bigint,uuid,text) TO photo_gateway;
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) RENAME TO commit_task_success_pre_v28;
REVOKE ALL ON FUNCTION photo.commit_task_success_pre_v28(uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_worker;
CREATE FUNCTION photo.commit_task_success(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 IF p_manifest IS NULL OR octet_length(p_manifest)>49152 THEN RAISE EXCEPTION 'INVALID_RESULT' USING ERRCODE='22023';END IF;
 t:=photo.lock_task(p_task);
 IF t.operation IN('portrait.hair','portrait.clothing') AND t.state='running' THEN
  IF t.worker_id IS DISTINCT FROM p_worker OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
   OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
   OR photo.v28_local_qualification_pin_ready(t.tenant_id,t.run_id,t.id) IS DISTINCT FROM true THEN
   RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;RETURN;
  END IF;
 END IF;
 RETURN QUERY SELECT * FROM photo.commit_task_success_pre_v28(p_task,p_attempt,p_fence,p_worker,p_manifest);
END $$;
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
