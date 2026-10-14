-- +goose Up
-- Session-free, parent-lease-bound counterpart of V31 lineage source scope.
-- It returns only the source run for the exact delegated Delivery slot and
-- exact frozen export pins; publication and task admission remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_lineage_scope(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_expected jsonb,p_bucket text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;pins jsonb;source_run uuid;project uuid;
BEGIN
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object'
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN NULL;END IF;
 SELECT t.run_id,asset.project_id,
  jsonb_build_object('project',asset.project_id,'version',v.id,'task',t.id,
   'run',t.run_id,'artifact',a.id,'sha256',a.sha256,'key',a.object_key,
   'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)
 INTO source_run,project,pins
 FROM photo.exports e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id
  AND v.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.projects p ON p.tenant_id=asset.tenant_id AND p.id=asset.project_id
 JOIN photo.tenants tenant_row ON tenant_row.id=e.tenant_id
 JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.id=e.task_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
  AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id
 WHERE e.tenant_id=(delegation->>'tenant_id')::uuid
  AND e.id=(delegation->>'export_id')::uuid AND e.status='ready'
  AND asset.deleted_at IS NULL AND p.archived_at IS NULL AND p.download_allowed
  AND tenant_row.status='active' AND t.state='succeeded'
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
  AND a.produced_fence=t.fencing_token
  AND z.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',a.id::text,'sha256',a.sha256))
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.bucket=p_bucket
  AND a.object_key<>'' AND a.bytes BETWEEN 1 AND 536870912
  AND a.media_type IN('image/jpeg','image/png')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF e,v,asset,p,tenant_row,t,z,a;
 IF NOT FOUND OR pins IS DISTINCT FROM p_expected
  OR project::text IS DISTINCT FROM delegation->>'project_id'
  OR pins->>'artifact' IS DISTINCT FROM delegation->>'export_artifact_id'
  OR pins->>'sha256' IS DISTINCT FROM delegation->>'export_sha' THEN RETURN NULL;END IF;
 RETURN source_run;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_lineage_scope(uuid,uuid,integer,uuid,jsonb,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
