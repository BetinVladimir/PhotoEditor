-- +goose Up
-- Revalidate the complete signed dependency matrix inside final assembly
-- activation. This gate does not enable required provenance by itself: the
-- signed assembly root, worker and publication path still need their contracts.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signing_assembly_gate(p_expansion uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;frozen jsonb;c photo.delivery_signing_closures%ROWTYPE;
BEGIN
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL;
 IF frozen IS NULL THEN RETURN false;END IF;
 IF frozen->'policy'->>'provenance'='off' THEN RETURN true;END IF;
 IF frozen->'policy'->>'provenance' IS DISTINCT FROM 'required' THEN RETURN false;END IF;
 SELECT c.* INTO c FROM photo.delivery_signing_closures c
 JOIN photo.delivery_packages d ON d.tenant_id=c.tenant_id AND d.id=c.package_id
 JOIN photo.v2_workflows w ON w.tenant_id=d.tenant_id AND w.id=d.workflow_id
 WHERE c.tenant_id=x.tenant_id AND c.parent_run_id=x.run_id AND w.run_id=x.run_id
  AND c.project_id=d.project_id AND d.state IN('building','review');
 IF NOT FOUND OR frozen->'policy'->>'signing_policy_id' IS DISTINCT FROM c.policy_id::text
  OR frozen->'policy'->>'signing_policy_sha' IS DISTINCT FROM c.policy_sha THEN RETURN false;END IF;
 RETURN photo.retain_v30_delivery_signing_closure(p_expansion,p_token,c.closure_sha,c.closure_jcs);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signing_assembly_gate(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signing_assembly_gate(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signing_assembly_gate(uuid,uuid) TO photo_scheduler;

-- Preserve the existing activation body and insert the new gate before its
-- expansion/run/task locks. Refuse an unexpected predecessor definition.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text:=' SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;';
BEGIN
 SELECT pg_get_functiondef('photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0
  OR position('photo.v30_delivery_signing_assembly_gate' IN definition)>0 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNING_ACTIVATION_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v30_delivery_signing_assembly_gate(p_expansion,p_token) IS DISTINCT FROM true THEN RETURN false;END IF;'||E'\n'||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
