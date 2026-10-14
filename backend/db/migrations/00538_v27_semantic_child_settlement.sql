-- +goose Up
CREATE INDEX semantic_targets_queued_child_idx ON photo.semantic_targets(tenant_id,child_run_id,id) WHERE state='queued';
-- Child completion settles only its admitted targets; parent review is independent.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_semantic_child_completion() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.state='queued' AND NEW.state IN('applied','failed','cancelled') AND NOT EXISTS(
  SELECT 1 FROM photo.runs r WHERE r.tenant_id=OLD.tenant_id AND r.id=OLD.child_run_id AND r.kind='apply'
  AND r.status=CASE NEW.state WHEN 'applied' THEN 'completed' ELSE NEW.state END
 ) THEN RAISE EXCEPTION 'SEMANTIC_CHILD_NOT_TERMINAL' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_semantic_child_completion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_semantic_child_completion() FROM PUBLIC;
CREATE TRIGGER semantic_target_child_completion BEFORE UPDATE ON photo.semantic_targets
FOR EACH ROW EXECUTE FUNCTION photo.guard_semantic_child_completion();

CREATE FUNCTION photo.settle_semantic_child(p_tenant uuid,p_child uuid) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE child_status text; target_state text; changed integer;
BEGIN
 -- Lifecycle precedes run and target locks. Purge cannot race these updates.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN 0;END IF;
 SELECT status INTO child_status FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply' FOR UPDATE;
 IF NOT FOUND OR child_status NOT IN('completed','failed','cancelled') THEN RETURN 0;END IF;
 target_state:=CASE child_status WHEN 'completed' THEN 'applied' WHEN 'failed' THEN 'failed' ELSE 'cancelled' END;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND child_run_id=p_child AND state='queued' ORDER BY id FOR UPDATE;
 UPDATE photo.semantic_targets SET state=target_state
 WHERE tenant_id=p_tenant AND child_run_id=p_child AND state='queued';
 GET DIAGNOSTICS changed=ROW_COUNT;
 RETURN changed;
END $$;
ALTER FUNCTION photo.settle_semantic_child(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.settle_semantic_child(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal;
GRANT EXECUTE ON FUNCTION photo.settle_semantic_child(uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.settle_next_semantic_child() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE r.kind='apply' AND r.status IN('completed','failed','cancelled')
 AND EXISTS(SELECT 1 FROM photo.semantic_targets t WHERE t.tenant_id=r.tenant_id AND t.child_run_id=r.id AND t.state='queued')
 ORDER BY r.tenant_id,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN 0;END IF;
 RETURN photo.settle_semantic_child(candidate.tenant_id,candidate.id);
END $$;
ALTER FUNCTION photo.settle_next_semantic_child() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.settle_next_semantic_child() FROM PUBLIC,photo_api,photo_worker,photo_portal;
GRANT EXECUTE ON FUNCTION photo.settle_next_semantic_child() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
