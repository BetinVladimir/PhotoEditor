-- +goose Up
-- Logical withdrawal only. Retained versions/snapshots/publications and shared
-- objects are NOT recursively purged, and no storage quota is released here.
ALTER TABLE photo.deletion_jobs ADD COLUMN resource_cleanup_retry_at timestamptz;
-- +goose StatementBegin
CREATE FUNCTION photo.withdrawal_runs(p_tenant uuid,p_kind text,p_resource uuid)
RETURNS SETOF uuid LANGUAGE sql STABLE SET search_path=pg_catalog,photo AS $$
 SELECT r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND (
 (p_kind='project' AND p_resource IN (
 SELECT project_id FROM photo.v2_workflows WHERE tenant_id=r.tenant_id AND run_id=r.id
 UNION SELECT project_id FROM photo.editor_effect_builds WHERE tenant_id=r.tenant_id AND (run_id=r.id OR final_run_id=r.id)
 UNION SELECT project_id FROM photo.semantic_targets WHERE tenant_id=r.tenant_id AND child_run_id=r.id
 UNION SELECT project_id FROM photo.collections WHERE tenant_id=r.tenant_id AND id=r.output_collection_id
 UNION SELECT project_id FROM photo.upload_sessions WHERE tenant_id=r.tenant_id AND ingest_run_id=r.id
 UNION SELECT project_id FROM photo.ingest_upload_bindings WHERE tenant_id=r.tenant_id AND run_id=r.id
 UNION SELECT c.project_id FROM photo.download_jobs d JOIN photo.collections c ON c.tenant_id=d.tenant_id AND c.id=d.collection_id WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id
 UNION SELECT c.project_id FROM photo.bulk_operations b JOIN photo.collections c ON c.tenant_id=b.tenant_id AND c.id=b.collection_id WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id
 UNION SELECT s.project_id FROM photo.styles s WHERE s.tenant_id=r.tenant_id AND s.id::text=r.config_snapshot->>'style_id'
 UNION SELECT s.project_id FROM photo.snapshots snap JOIN photo.training_sets t ON t.tenant_id=snap.tenant_id AND t.id=snap.training_set_id JOIN photo.styles s ON s.tenant_id=t.tenant_id AND s.id=t.style_id WHERE snap.tenant_id=r.tenant_id AND snap.id=r.input_snapshot_id
 UNION SELECT c.project_id FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id WHERE s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 UNION SELECT a.project_id FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 UNION SELECT a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=r.tenant_id AND v.id::text=r.config_snapshot->>'asset_version_id'
 UNION SELECT p.id FROM photo.projects p WHERE p.tenant_id=r.tenant_id AND p.id::text=r.config_snapshot->>'project_id'
 )) OR (p_kind='asset' AND (
 EXISTS(SELECT FROM photo.asset_versions v WHERE v.tenant_id=p_tenant AND v.asset_id=p_resource AND (
 v.id::text=r.config_snapshot->>'asset_version_id'
 OR EXISTS(SELECT FROM photo.snapshot_items i WHERE i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id AND i.asset_version_id=v.id)
 OR EXISTS(SELECT FROM photo.editor_effect_builds b WHERE b.tenant_id=r.tenant_id AND (b.run_id=r.id OR b.final_run_id=r.id) AND b.asset_version_id=v.id)
 OR EXISTS(SELECT FROM photo.semantic_targets s WHERE s.tenant_id=r.tenant_id AND s.child_run_id=r.id AND s.asset_version_id=v.id)
 OR EXISTS(SELECT FROM photo.semantic_targets s JOIN photo.semantic_intents i ON i.tenant_id=s.tenant_id AND i.id=s.intent_id WHERE s.tenant_id=r.tenant_id AND s.child_run_id=r.id AND i.reference_version_id=v.id)
 OR EXISTS(SELECT FROM photo.semantic_intents s JOIN photo.v2_workflows w ON w.tenant_id=s.tenant_id AND w.id=s.workflow_id WHERE s.tenant_id=r.tenant_id AND w.run_id=r.id AND s.reference_version_id=v.id)
 OR EXISTS(SELECT FROM photo.bulk_operations b JOIN photo.snapshot_items i ON i.tenant_id=b.tenant_id AND i.snapshot_id=b.snapshot_id WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND i.asset_version_id=v.id)
 OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',v.master_artifact_id::text,'sha256',v.content_hash)))
 )) OR EXISTS(SELECT FROM photo.ingest_upload_bindings b WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND b.asset_id=p_resource)
 )))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.withdrawal_runs(uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdrawal_runs(uuid,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_maintenance,photo_portal,photo_auth,photo_gateway,photo_publisher;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_resource_withdrawal() RETURNS TABLE(job_id uuid,cleanup_token uuid,tenant_id uuid,resource_type text,resource_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE; token uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO j FROM photo.deletion_jobs x JOIN photo.tenants t ON t.id=x.tenant_id AND t.status='active'
 WHERE x.resource_type IN('project','asset') AND (x.resource_cleanup_retry_at IS NULL OR x.resource_cleanup_retry_at<=clock_timestamp())
 AND (x.status='queued' OR (x.status='running' AND x.cleanup_lease_until<=clock_timestamp()))
 AND ((x.resource_type='project' AND EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=x.tenant_id AND p.id=x.resource_id AND p.archived_at IS NOT NULL))
 OR (x.resource_type='asset' AND EXISTS(SELECT FROM photo.assets a WHERE a.tenant_id=x.tenant_id AND a.id=x.resource_id AND a.deleted_at IS NOT NULL)))
 ORDER BY coalesce(x.resource_cleanup_retry_at,x.created_at),x.id LIMIT 1 FOR UPDATE OF x SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.deletion_jobs x SET status='running',cleanup_token=token,cleanup_lease_until=clock_timestamp()+interval '30 seconds' WHERE x.tenant_id=j.tenant_id AND x.id=j.id;
 RETURN QUERY SELECT j.id,token,j.tenant_id,j.resource_type,j.resource_id;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.finish_resource_withdrawal(p_tenant uuid,p_job uuid,p_kind text,p_resource uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE; ids uuid[]; blocked boolean;
BEGIN
 IF p_kind NOT IN('project','asset') OR p_token IS NULL THEN RETURN false;END IF;
 SELECT x.* INTO j FROM photo.deletion_jobs x WHERE x.tenant_id=p_tenant AND x.id=p_job AND x.resource_type=p_kind AND x.resource_id=p_resource
 AND x.status='running' AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_kind='project' THEN
  PERFORM FROM photo.projects WHERE tenant_id=p_tenant AND id=p_resource AND archived_at IS NOT NULL;
 ELSE
  PERFORM FROM photo.assets WHERE tenant_id=p_tenant AND id=p_resource AND deleted_at IS NOT NULL;
 END IF;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(id ORDER BY id) INTO ids FROM photo.withdrawal_runs(p_tenant,p_kind,p_resource) id;
 -- Do not lock asset/project here: adopted-version application can hold its run
 -- before updating the asset. Archive/delete are irreversible API tombstones;
 -- repeat their exact existence predicate after all waits instead of inverting
 -- that order. No retained resource metadata is changed by this consumer.
 PERFORM FROM photo.runs WHERE tenant_id=p_tenant AND id=ANY(coalesce(ids,'{}'::uuid[])) ORDER BY id FOR UPDATE;
 IF j.cleanup_lease_until<=clock_timestamp() OR NOT EXISTS(SELECT FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 UPDATE photo.runs SET cancel_requested=true,status=CASE WHEN status IN('draft','queued','running','waiting_review') THEN 'cancelling' ELSE status END,revision=revision+1
 WHERE tenant_id=p_tenant AND id=ANY(coalesce(ids,'{}'::uuid[])) AND status IN('draft','queued','running','waiting_review','cancelling') AND NOT cancel_requested;
 IF p_kind='project' THEN
  UPDATE photo.download_jobs d SET cancel_requested=true,status=CASE WHEN status IN('queued','preparing','building','uploading') THEN 'cancelling' ELSE status END
  WHERE d.tenant_id=p_tenant AND d.status NOT IN('ready','failed','cancelled','expired') AND d.collection_id IN(SELECT id FROM photo.collections WHERE tenant_id=p_tenant AND project_id=p_resource);
  UPDATE photo.bulk_operations SET cancel_requested=true WHERE tenant_id=p_tenant AND status NOT IN('completed','completed_with_errors','failed','cancelled') AND collection_id IN(SELECT id FROM photo.collections WHERE tenant_id=p_tenant AND project_id=p_resource);
  UPDATE photo.upload_sessions SET expires_at=least(expires_at,clock_timestamp()) WHERE tenant_id=p_tenant AND project_id=p_resource AND state IN('initializing','uploading');
 END IF;
 SELECT EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=ANY(coalesce(ids,'{}'::uuid[])) AND (r.status IN('draft','queued','running','waiting_review','cancelling') OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped')))) INTO blocked;
 IF p_kind='project' THEN
  blocked:=blocked OR EXISTS(SELECT FROM photo.upload_sessions WHERE tenant_id=p_tenant AND project_id=p_resource AND state IN('initializing','uploading','completing','verifying'))
   OR EXISTS(SELECT FROM photo.download_jobs d WHERE d.tenant_id=p_tenant AND d.status NOT IN('ready','failed','cancelled','expired') AND d.collection_id IN(SELECT id FROM photo.collections WHERE tenant_id=p_tenant AND project_id=p_resource))
   OR EXISTS(SELECT FROM photo.bulk_operations WHERE tenant_id=p_tenant AND status NOT IN('completed','completed_with_errors','failed','cancelled') AND collection_id IN(SELECT id FROM photo.collections WHERE tenant_id=p_tenant AND project_id=p_resource));
 END IF;
 -- All preceding lock waits/mutations precede the final lease and tenant fence.
 IF j.cleanup_lease_until<=clock_timestamp() OR NOT EXISTS(SELECT FROM photo.tenants WHERE id=p_tenant AND status='active')
 OR (p_kind='project' AND NOT EXISTS(SELECT FROM photo.projects WHERE tenant_id=p_tenant AND id=p_resource AND archived_at IS NOT NULL))
 OR (p_kind='asset' AND NOT EXISTS(SELECT FROM photo.assets WHERE tenant_id=p_tenant AND id=p_resource AND deleted_at IS NOT NULL))
 THEN RAISE EXCEPTION 'resource withdrawal scope or lease expired' USING ERRCODE='40001';END IF;
 IF blocked THEN
  UPDATE photo.deletion_jobs SET status='queued',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,resource_cleanup_retry_at=clock_timestamp()+interval '5 seconds' WHERE tenant_id=p_tenant AND id=p_job;
  RETURN false;
 END IF;
 UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,error=NULL,
 cursor='{"phase":"logical_withdrawal_settled","physical_cleanup":"reference_and_retention_governed"}' WHERE tenant_id=p_tenant AND id=p_job;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_resource_withdrawal() OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_resource_withdrawal(),photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_auth,photo_gateway,photo_publisher;
GRANT EXECUTE ON FUNCTION photo.claim_resource_withdrawal(),photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
