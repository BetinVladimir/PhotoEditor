-- Exact authored native helper bodies, test-only installation; no lifecycle calls.
CREATE OR REPLACE FUNCTION photo.authored_mask_lock_execution_parent(p_task uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;again jsonb;
BEGIN
 d:=photo.authored_execution_parent_domain(p_task);
 IF d IS NULL THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=(d->>'tenant')::uuid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AUTHORED_MASK_EXECUTION_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=(d->>'tenant')::uuid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AUTHORED_MASK_EXECUTION_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=(d->>'tenant')::uuid AND id=(d->>'parent')::uuid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AUTHORED_MASK_EXECUTION_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 again:=photo.authored_execution_parent_domain(p_task);
 IF again IS DISTINCT FROM d THEN RAISE EXCEPTION 'AUTHORED_MASK_EXECUTION_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 -- All original lease/current/attempt/identity predicates remain mandatory.
END $$;
ALTER FUNCTION photo.authored_mask_lock_execution_parent(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authored_mask_lock_execution_parent(uuid) FROM PUBLIC,photo_api,photo_auth,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;

CREATE OR REPLACE FUNCTION photo.authored_native_replay_parent_lock(p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $body$
DECLARE d jsonb;
BEGIN
 PERFORM photo.authored_mask_lock_execution_parent(p_task);
 d:=photo.authored_execution_parent_domain(p_task);
 RETURN d IS NOT NULL;
END $body$;
ALTER FUNCTION photo.authored_native_replay_parent_lock(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authored_native_replay_parent_lock(uuid) FROM PUBLIC,photo_api,photo_auth,photo_worker,photo_gateway,photo_scheduler,photo_maintenance,photo_portal,photo_semantic_admission;
