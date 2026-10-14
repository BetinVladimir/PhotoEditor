-- +goose Up
-- Parent cancellation is authoritative only after its own tasks are terminal.
-- +goose StatementBegin
CREATE FUNCTION photo.sync_v30_delivery_cancellation(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d photo.delivery_packages%ROWTYPE;event_id uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'cancelled' OR NOT r.graph_sealed THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN('succeeded','failed','cancelled','skipped')) THEN RETURN false;END IF;
 SELECT p.* INTO d FROM photo.delivery_packages p JOIN photo.v2_workflows w ON w.tenant_id=p.tenant_id AND w.id=p.workflow_id
 WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.workflow_type='delivery' FOR UPDATE OF p;
 IF NOT FOUND OR d.published_at IS NOT NULL OR d.state NOT IN('building','review') THEN RETURN false;END IF;
 IF d.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.delivery_packages SET state='cancelled',revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 -- Retain immutable manifest/source references and independent child lifecycle.
 -- Quota remains until documents/uploads are physically ready or cleaned.
 event_id:=photo.v30_uuid5(p_run,'delivery:cancelled:1');
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','delivery.cancelled',jsonb_build_object('schema_version',1,'event_id',event_id,
 'type','delivery.cancelled','tenant_id',p_tenant,'run_id',p_run,'package_id',d.id,'status','cancelled'));
 RETURN true;
END $$;
ALTER FUNCTION photo.sync_v30_delivery_cancellation(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.sync_v30_delivery_cancellation(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.sync_v30_delivery_cancellation(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.cleanup_next_v30_delivery() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;changed boolean:=false;released boolean;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id AND w.workflow_type='delivery'
 JOIN photo.delivery_packages d ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id
 WHERE r.kind='v2' AND r.status='cancelled' AND r.graph_sealed AND d.state IN('building','review') AND d.published_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF FOUND THEN changed:=photo.sync_v30_delivery_cancellation(candidate.tenant_id,candidate.id);END IF;
 released:=photo.release_next_v30_delivery_quota();
 RETURN changed OR released;
END $$;
ALTER FUNCTION photo.cleanup_next_v30_delivery() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cleanup_next_v30_delivery() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.cleanup_next_v30_delivery() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
