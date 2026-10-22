-- +goose Up
-- Final activation fence covers V2 operations embedded in Apply as well as kind=v2.
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_before_v2_activation_guard;
REVOKE ALL ON FUNCTION photo.claim_task_before_v2_activation_guard(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_worker,photo_api,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record; task photo.tasks;
BEGIN
 SELECT * INTO claimed FROM photo.claim_task_before_v2_activation_guard(p_task,p_attempt,p_worker,p_command);
 -- Completed replay, stale and busy retain their existing identity semantics.
 IF claimed.outcome='claimed' THEN
  SELECT * INTO STRICT task FROM photo.tasks WHERE id=p_task;
  IF task.operation IN ('layer.compose','session.describe','session.plan','learning.fit','learning.evaluate','capture.assess','identity.compare','semantic.transfer','portrait.open_eyes','portrait.glare','image.reflection','portrait.hair','portrait.clothing','image.relight','optical.lens','optical.perspective','optical.negative','optical.panorama','optical.focus_stack','provenance.sign','v2.delivery.assemble','v2.interchange.import','v2.interchange.export','v2.semantic.assemble','v2.interchange.parse','v2.delivery.prepare','region.preview','render.compose','semantic.local_exposure','optical.perspective_lines','semantic.adopt_shown_exposure','semantic.preview_remove_object','semantic.adopt_shown_removal','semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region','semantic.preview_retouch_region') THEN
   -- Hold the current registry row through lease/start commit. A revocation
   -- before this point rejects and rolls back every delegated claim mutation.
   PERFORM operation FROM photo.operation_registry
   WHERE operation=task.operation AND version=task.operation_version AND v2_enabled
    AND resource_class=task.resource_class
    AND backends ? (task.execution_profile->>'backend') FOR SHARE;
   IF NOT FOUND THEN
    RAISE EXCEPTION 'V2_OPERATION_CLAIM_ACTIVATION_STALE' USING ERRCODE='40001';
   END IF;
  END IF;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
