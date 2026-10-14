-- +goose Up
-- The scheduler bucket must be the bucket of this package's immutable input
-- documents, not merely the bucket on the scheduler's own runtime lease.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_expansion_bucket_ready(p_bucket text,p_expansion uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT length(p_bucket) BETWEEN 3 AND 63 AND EXISTS(
  SELECT 1 FROM photo.graph_expansions e
  JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.run_id=e.run_id
   AND t.id=e.parent_task_id AND t.operation='v2.delivery.prepare'
  JOIN photo.backend_documents b ON b.tenant_id=e.tenant_id AND b.run_id=e.run_id
   AND b.owner_id=t.id AND b.purpose='task_params'
   AND b.artifact_id=t.params_artifact_id AND b.state='ready' AND b.bucket=p_bucket
  JOIN photo.v2_workflows w ON w.tenant_id=e.tenant_id AND w.run_id=e.run_id
   AND w.workflow_type='delivery' AND w.renderer_version='v2.2'
  JOIN photo.delivery_packages d ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id
   AND d.project_id=w.project_id AND d.snapshot_id=w.input_snapshot_id
   AND d.state='building' AND d.manifest_artifact_id IS NULL
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.policy_artifact_id
   AND a.bucket=p_bucket AND a.sha256=w.policy_sha AND a.state='ready'
   AND a.media_type='application/json' AND a.object_version IS NOT NULL
   AND a.verified_at IS NOT NULL
  WHERE e.id=p_expansion AND e.kind='v2.delivery/1' AND e.state='open');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_expansion_bucket_ready(text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_bucket_ready(text,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- Gate both ordinary and signed expansion claims before inspecting their
-- frozen signing policy. Keep the existing instance/policy routing unchanged.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)'::regprocedure);
 needle:=' SELECT e.tenant_id,(b.v30_delivery_proof->>''frozen_input_jcs'')::jsonb->''policy''';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected V30 delivery instance gate shape';END IF;
 definition:=replace(definition,needle,
  ' IF photo.v30_delivery_expansion_bucket_ready(p_bucket,p_expansion) IS DISTINCT FROM true '
  ||'THEN RETURN false;END IF;'||needle);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_expansion_instance_ready(uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;

-- Recheck package bucket inside the signed terminal transaction, after it
-- obtains the caller's live instance bucket and before it reads policy pins.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)'::regprocedure);
 needle:=' IF bucket IS NULL THEN RETURN false;END IF;';
 IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected V31 policy failure bucket gate shape';END IF;
 definition:=replace(definition,needle,needle||
  ' IF photo.v30_delivery_expansion_bucket_ready(bucket,p_expansion) IS DISTINCT FROM true '
  ||'THEN RETURN false;END IF;');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_v31_delivery_expired_policy(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
