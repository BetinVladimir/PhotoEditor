-- +goose Up
-- Frozen recovery payload is available only to the holder of a current lease.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_expansion_plan(p_expansion uuid,p_token uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;b photo.backend_documents%ROWTYPE;input_ref jsonb;
BEGIN
 IF p_token IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT x.* INTO e FROM photo.graph_expansions x WHERE x.id=p_expansion AND x.tenant_id=e.tenant_id FOR UPDATE;
 IF NOT FOUND OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL
 OR e.expansion_lease_until<=clock_timestamp() OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=e.parent_task_id AND run_id=e.run_id;
 SELECT * INTO b FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO input_ref FROM photo.v2_task_inputs
 WHERE tenant_id=e.tenant_id AND task_id=t.id AND contract='FrozenDeliveryInput';
 IF b.id IS NULL OR b.v30_delivery_proof IS NULL OR input_ref IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('proof',b.v30_delivery_proof,'params',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',b.sha256),'input',input_ref);
END $$;
ALTER FUNCTION photo.v30_delivery_expansion_plan(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_plan(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_expansion_plan(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
