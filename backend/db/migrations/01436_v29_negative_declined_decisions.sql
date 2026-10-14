-- +goose Up
-- Durable retry source is the immutable decided review, not an in-memory event.
-- This reader grants no mutation authority; reconcile repeats all checks.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_declined_decisions(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_after uuid,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('review_id',d.id,'origin_task_id',d.task_id,
 'origin_run_id',d.run_id,'decision_hash',d.decision_hash,'route',d.decision_route) ORDER BY d.id),'[]'::jsonb)
 INTO result FROM (
 SELECT v.id,v.task_id,v.run_id,v.decision_hash,v.decision_route FROM photo.reviews v
 JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
 WHERE v.tenant_id=p_tenant AND w.project_id=p_project AND v.kind='optical.negative'
 AND v.state='decided' AND v.revision=2 AND (p_after IS NULL OR v.id>p_after)
 AND ((v.decision='reject' AND v.decision_route='base') OR (v.decision='skip' AND v.decision_route='skip'))
 ORDER BY v.id LIMIT p_limit) d;
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_declined_decisions(uuid,uuid,uuid,uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_declined_decisions(uuid,uuid,uuid,uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_declined_decisions(uuid,uuid,uuid,uuid,uuid,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
