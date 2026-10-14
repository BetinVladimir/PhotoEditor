-- +goose Up
-- Conservative tenant-scoped writer barrier for the complete person inventory.
-- AFTER row mutation prevents commit crossing the admission barrier. Deadlocks
-- abort a transaction; callers must rollback/retry the whole durable operation.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v27_person_inventory_write() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE previous jsonb:='{}'; following jsonb:='{}'; scope uuid;
BEGIN
 IF TG_OP IN('UPDATE','DELETE') THEN previous:=to_jsonb(OLD); END IF;
 IF TG_OP IN('INSERT','UPDATE') THEN following:=to_jsonb(NEW); END IF;
 FOR scope IN SELECT DISTINCT value FROM (VALUES(nullif(previous->>'tenant_id','')::uuid),(nullif(following->>'tenant_id','')::uuid)) ids(value) WHERE value IS NOT NULL ORDER BY value
 LOOP PERFORM pg_advisory_xact_lock(hashtextextended('photo.v27.person.inventory:'||scope::text,0)); END LOOP;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.guard_v27_person_inventory_write() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v27_person_inventory_write() FROM PUBLIC;
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.artifacts FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.tasks FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.task_attempts FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.task_results FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.v2_workflows FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.v2_task_inputs FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.apply_nodes FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE TRIGGER v27_person_inventory_write AFTER INSERT OR UPDATE OR DELETE ON photo.interactive_tasks FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_person_inventory_write();
CREATE FUNCTION photo.lock_semantic_proposal_person_inventory(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('photo.v27.person.inventory:'||p_tenant::text,0));
 RETURN photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_person_inventory(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_person_inventory(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_person_inventory(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
