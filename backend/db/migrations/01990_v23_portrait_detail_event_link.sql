-- The correction event is immutable. Bind a portrait.detail producer in the
-- same transaction that creates a new event; historical events are not
-- retroactively linked by matching a source hash.
-- +goose Up
CREATE TABLE photo.v23_portrait_detail_event_links (
 tenant_id uuid NOT NULL,
 project_id uuid NOT NULL,
 event_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 run_id uuid NOT NULL,
 task_id uuid NOT NULL,
 result_id uuid NOT NULL,
 attempt_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,event_id),
 FOREIGN KEY(tenant_id,project_id,event_id)
  REFERENCES photo.correction_events(tenant_id,project_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,task_id) REFERENCES photo.tasks(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,result_id) REFERENCES photo.task_results(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,attempt_id) REFERENCES photo.task_attempts(tenant_id,id)
  DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.v23_portrait_detail_event_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v23_portrait_detail_event_links FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v23_portrait_detail_event_links
 TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v23_portrait_detail_event_links FROM PUBLIC,
 photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,
 photo_maintenance;
GRANT SELECT,INSERT,DELETE ON photo.v23_portrait_detail_event_links
 TO photo_fn_owner;
CREATE TRIGGER v23_portrait_detail_event_links_immutable BEFORE UPDATE ON
 photo.v23_portrait_detail_event_links FOR EACH ROW
 EXECUTE FUNCTION photo.guard_v2_immutable();

-- +goose StatementBegin
CREATE FUNCTION photo.bind_v23_portrait_detail_event(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid,
 p_run uuid,p_task uuid,p_result uuid,p_attempt uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.correction_events%ROWTYPE; event_xid bigint;
 source_row record; saved photo.v23_portrait_detail_event_links%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL OR p_run IS NULL OR
  p_task IS NULL OR p_result IS NULL OR p_attempt IS NULL THEN
  RETURN false;
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active'
  FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(
  p_tenant,p_project,p_actor,p_auth_session) THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.correction_events x WHERE x.tenant_id=p_tenant
  AND x.project_id=p_project AND x.id=p_event AND x.approved_by=p_actor
  AND x.learn FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT x.xmin::text::bigint INTO event_xid
  FROM photo.correction_events x WHERE x.tenant_id=p_tenant AND x.id=p_event;
 SELECT * INTO source_row FROM photo.v23_portrait_detail_event_producer(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND OR source_row.run_id IS DISTINCT FROM p_run OR
  source_row.task_id IS DISTINCT FROM p_task OR
  source_row.result_id IS DISTINCT FROM p_result OR
  source_row.attempt_id IS DISTINCT FROM p_attempt THEN RETURN false;END IF;
 SELECT * INTO saved FROM photo.v23_portrait_detail_event_links x
  WHERE x.tenant_id=p_tenant AND x.event_id=p_event FOR SHARE;
 IF FOUND THEN
  RETURN saved.project_id=p_project AND saved.actor_id=p_actor AND
   saved.run_id=p_run AND saved.task_id=p_task AND
   saved.result_id=p_result AND saved.attempt_id=p_attempt;
 END IF;
 -- xmin is the inserting transaction for an immutable correction event.
 -- Reject late adoption of historical rows even when the Apply path matches.
 IF event_xid IS NULL OR event_xid<>(txid_current()%4294967296)
  THEN RETURN false;END IF;
 INSERT INTO photo.v23_portrait_detail_event_links(
  tenant_id,project_id,event_id,actor_id,run_id,task_id,result_id,attempt_id)
 VALUES(p_tenant,p_project,p_event,p_actor,p_run,p_task,p_result,p_attempt);
 RETURN true;
EXCEPTION WHEN unique_violation OR foreign_key_violation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.bind_v23_portrait_detail_event(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v23_portrait_detail_event(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_v23_portrait_detail_event(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_portrait_detail_event_link_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid)
RETURNS TABLE(asset_version_id uuid,source_id uuid,source_sha text,
 consent_id uuid,consent_epoch bigint,run_id uuid,task_id uuid,
 result_id uuid,attempt_id uuid,image_id uuid,image_sha text,
 params_id uuid,params_sha text,resolved_id uuid,resolved_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record; saved photo.v23_portrait_detail_event_links%ROWTYPE;
BEGIN
 SELECT * INTO saved FROM photo.v23_portrait_detail_event_links x
  WHERE x.tenant_id=p_tenant AND x.project_id=p_project AND
   x.event_id=p_event FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO selected FROM photo.v23_portrait_detail_event_producer(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND OR selected.run_id IS DISTINCT FROM saved.run_id OR
  selected.task_id IS DISTINCT FROM saved.task_id OR
  selected.result_id IS DISTINCT FROM saved.result_id OR
  selected.attempt_id IS DISTINCT FROM saved.attempt_id THEN RETURN;END IF;
 RETURN QUERY SELECT selected.asset_version_id,selected.source_id,
  selected.source_sha,selected.consent_id,selected.consent_epoch,
  selected.run_id,selected.task_id,selected.result_id,selected.attempt_id,
  selected.image_id,selected.image_sha,selected.params_id,
  selected.params_sha,selected.resolved_id,selected.resolved_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_portrait_detail_event_link_source(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_portrait_detail_event_link_source(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_portrait_detail_event_link_source(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- New tenant metadata joins the object-cleanup-gated physical purge.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 anchor constant text := '''v29_perspective_lines'',';
 entry constant text := '''v23_portrait_detail_event_links'',';
BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)
  INTO definition;
 IF definition IS NULL OR strpos(definition,anchor)=0 OR
  strpos(definition,entry)>0 OR
  strpos(definition,
   'cleanup_token=p_token AND cleanup_lease_until>clock_timestamp()')=0
  THEN RAISE EXCEPTION 'V23_DETAIL_EVENT_PURGE_SHAPE';END IF;
 EXECUTE replace(definition,anchor,entry||anchor);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
