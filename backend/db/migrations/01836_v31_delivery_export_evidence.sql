-- +goose Up
-- Exact session-free source metadata for a delegated V31 child. The final
-- predicate repeats the full V31 export graph and parent lease checks.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_export_evidence(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;pins jsonb;candidate jsonb;source_run uuid;
BEGIN
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('project',asset.project_id,'version',v.id,
  'task',t.id,'run',t.run_id,'artifact',a.id,'sha256',a.sha256,
  'key',a.object_key,'object_version',a.object_version,
  'bytes',a.bytes,'media_type',a.media_type),
  jsonb_build_object('tenant_id',e.tenant_id,'project_id',asset.project_id,
   'export_id',e.id,'version_id',v.id,'task_id',t.id,'run_id',t.run_id,
   'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,
   'object_key',a.object_key,'object_version',a.object_version,
   'bytes',a.bytes,'media_type',a.media_type)
 INTO pins,candidate
 FROM photo.exports e JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id
  AND v.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.tasks t ON t.tenant_id=e.tenant_id AND t.id=e.task_id
 JOIN photo.artifacts a ON a.tenant_id=e.tenant_id AND a.id=e.artifact_id
 WHERE e.tenant_id=(delegation->>'tenant_id')::uuid
  AND e.id=(delegation->>'export_id')::uuid
  AND asset.project_id=(delegation->>'project_id')::uuid
  AND a.id=(delegation->>'export_artifact_id')::uuid
  AND a.sha256=delegation->>'export_sha' FOR SHARE OF e,v,asset,t,a;
 IF NOT FOUND OR candidate->>'bucket' IS NULL THEN RETURN NULL;END IF;
 source_run:=photo.v31_delivery_lineage_scope(p_expansion,p_token,p_ordinal,
  p_preset,pins,candidate->>'bucket');
 IF source_run IS NULL OR source_run::text IS DISTINCT FROM candidate->>'run_id'
  THEN RETURN NULL;END IF;
 RETURN candidate;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_export_evidence(uuid,uuid,integer,uuid)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
