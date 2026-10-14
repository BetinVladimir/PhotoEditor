-- +goose Up
-- Release remaining reservation after durable successful assembly, including review.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.release_v30_delivery_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status NOT IN('failed','cancelled','waiting_review','completed')
 OR NOT r.graph_sealed OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.delivery_packages d
 ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id WHERE w.tenant_id=p_tenant AND w.run_id=p_run
 AND w.workflow_type='delivery' AND (
 (r.status IN('failed','cancelled') AND d.state=r.status AND d.published_at IS NULL)
 OR (r.status='waiting_review' AND d.state='review' AND d.manifest_artifact_id IS NOT NULL
 AND photo.v30_delivery_assembly_adoption_committed(p_tenant,photo.v30_uuid5(w.id,'v2.delivery.assemble:0')))
 OR (r.status='completed' AND d.state='ready' AND d.manifest_artifact_id IS NOT NULL
 AND photo.v30_delivery_assembly_adoption_committed(p_tenant,photo.v30_uuid5(w.id,'v2.delivery.assemble:0'))))) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN true;END IF;
 IF q.state='released' AND q.bytes=0 THEN RETURN true;END IF;
 IF q.state<>'reserved' OR q.principal_type<>'user' OR q.principal_id<>r.created_by THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
 RETURN true;
END $$;

CREATE OR REPLACE FUNCTION photo.release_next_v30_delivery_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.workflow_type='delivery'
 JOIN photo.delivery_packages d ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id AND (
 (r.status IN('failed','cancelled') AND d.state=r.status AND d.published_at IS NULL)
 OR (r.status='waiting_review' AND d.state='review' AND d.manifest_artifact_id IS NOT NULL
 AND photo.v30_delivery_assembly_adoption_committed(r.tenant_id,photo.v30_uuid5(w.id,'v2.delivery.assemble:0')))
 OR (r.status='completed' AND d.state='ready' AND d.manifest_artifact_id IS NOT NULL
 AND photo.v30_delivery_assembly_adoption_committed(r.tenant_id,photo.v30_uuid5(w.id,'v2.delivery.assemble:0'))))
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 WHERE r.kind='v2' AND r.status IN('failed','cancelled','waiting_review','completed') AND r.graph_sealed
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND b.state NOT IN('ready','cleaned'))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.release_v30_delivery_quota(candidate.tenant_id,candidate.id);
END $$;
ALTER FUNCTION photo.release_next_v30_delivery_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v30_delivery_quota() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.release_next_v30_delivery_quota() TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
