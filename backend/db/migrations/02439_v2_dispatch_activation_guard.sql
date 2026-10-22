-- +goose Up
-- Preserve operation-specific dispatch/qualification/reservation checks and add
-- a final fence for V2 operations embedded in any run kind, including Apply.
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 RENAME TO reserve_dispatch_before_v2_activation_guard;
REVOKE ALL ON FUNCTION photo.reserve_dispatch_before_v2_activation_guard(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_api,photo_gateway,photo_portal,photo_semantic_admission;
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,
 p_event uuid,p_correlation uuid,p_trace text) RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE reserved record; task photo.tasks;
BEGIN
 SELECT * INTO reserved FROM photo.reserve_dispatch_before_v2_activation_guard(
  p_task,p_node,p_ram,p_vram,p_scratch,p_event,p_correlation,p_trace);
 IF reserved.outcome='queued' THEN
  SELECT * INTO STRICT task FROM photo.tasks WHERE id=p_task;
  IF task.operation IN ('layer.compose','session.describe','session.plan','learning.fit','learning.evaluate','capture.assess','identity.compare','semantic.transfer','portrait.open_eyes','portrait.glare','image.reflection','portrait.hair','portrait.clothing','image.relight','optical.lens','optical.perspective','optical.negative','optical.panorama','optical.focus_stack','provenance.sign','v2.delivery.assemble','v2.interchange.import','v2.interchange.export','v2.semantic.assemble','v2.interchange.parse','v2.delivery.prepare','region.preview','render.compose','semantic.local_exposure','optical.perspective_lines','semantic.adopt_shown_exposure','semantic.preview_remove_object','semantic.adopt_shown_removal','semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region','semantic.preview_retouch_region') THEN
   PERFORM operation FROM photo.operation_registry
   WHERE operation=task.operation AND version=task.operation_version AND v2_enabled
    AND resource_class=task.resource_class
    AND backends ? (task.execution_profile->>'backend') FOR SHARE;
   IF NOT FOUND THEN
    RAISE EXCEPTION 'V2_OPERATION_DISPATCH_ACTIVATION_STALE' USING ERRCODE='40001';
   END IF;
  END IF;
 END IF;
 RETURN QUERY SELECT reserved.outcome,reserved.command;
END $$;
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
