-- +goose Up
-- Scheduler-only parent actor read for deterministic child status validation.
-- It does not create an editor session or authorize signing by itself.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_signing_parent_actor(
 p_expansion uuid,p_token uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;frozen jsonb;
BEGIN
 IF p_expansion IS NULL OR p_token IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion
  AND kind='v2.delivery/1' AND state='open'
  AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()
  FOR SHARE;
 IF NOT FOUND OR x.expected_count NOT BETWEEN 1 AND 80000
  OR x.cursor IS NULL OR x.cursor !~ '^[0-9]+$' THEN RETURN NULL;END IF;
 IF x.cursor::bigint<>x.expected_count THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=x.tenant_id AND id=x.run_id
  AND kind='v2' AND status='running' AND NOT cancel_requested
  AND created_by IS NOT NULL FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.tenants t
  WHERE t.id=x.tenant_id AND t.status='active') THEN RETURN NULL;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params'
  AND b.state='ready' AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_id' IS NULL
  OR frozen->'policy'->>'signing_policy_sha' !~ '^[0-9a-f]{64}$'
  THEN RETURN NULL;END IF;
 RETURN r.created_by;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_signing_parent_actor(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_signing_parent_actor(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_signing_parent_actor(uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
