-- +goose Up
-- Read-back is scheduler-only and lease-fenced. The caller must recompile the
-- complete current slot/receipt set and compare exact canonical bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_delivery_signing_closure(p_expansion uuid,p_token uuid)
RETURNS TABLE(closure_jcs text,closure_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint_tenant uuid;hint_run uuid;x photo.graph_expansions%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;c photo.delivery_signing_closures%ROWTYPE;frozen jsonb;
BEGIN
 SELECT tenant_id,run_id INTO hint_tenant,hint_run FROM photo.graph_expansions
  WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=hint_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=hint_tenant AND id=hint_run AND status='running'
  AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND tenant_id=hint_tenant
  AND run_id=hint_run AND kind='v2.delivery/1' AND state='open'
  AND expansion_token=p_token AND expansion_lease_until>clock_timestamp() FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND workflow_type='delivery' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=x.tenant_id AND workflow_id=w.id
  AND state IN('building','review') FOR SHARE;
 IF NOT FOUND OR d.project_id<>w.project_id THEN RETURN;END IF;
 SELECT * INTO c FROM photo.delivery_signing_closures WHERE tenant_id=x.tenant_id
  AND project_id=d.project_id AND package_id=d.id AND parent_run_id=x.run_id FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_id' IS DISTINCT FROM c.policy_id::text
  OR frozen->'policy'->>'signing_policy_sha' IS DISTINCT FROM c.policy_sha
  OR encode(sha256(convert_to(c.closure_jcs,'UTF8')),'hex')<>c.closure_sha
  OR photo.v31_signing_policy_ready(c.tenant_id,c.policy_id,
   photo.v30_uuid5(c.tenant_id,'signing:policy:'||c.policy_id::text||':'||c.policy_sha),c.policy_sha)
    IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT c.closure_jcs,c.closure_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v30_delivery_signing_closure(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_delivery_signing_closure(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.read_v30_delivery_signing_closure(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
