-- +goose Up
-- Scheduler service mode enforces an immutable editorial decision. It cannot
-- manufacture a decision or admit replacement content. User mode still requires
-- a live editor session. Both modes require active tenant and project.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_invalidation_authorized(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tenants t JOIN photo.projects p ON p.tenant_id=t.id
 WHERE t.id=p_tenant AND t.status='active' AND p.id=p_project AND p.archived_at IS NULL)
 AND CASE WHEN p_actor IS NULL AND p_session IS NULL THEN true
 WHEN p_actor IS NULL OR p_session IS NULL THEN false
 ELSE photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) END
$$;
ALTER FUNCTION photo.v29_negative_invalidation_authorized(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_invalidation_authorized(uuid,uuid,uuid,uuid) FROM PUBLIC;
DO $$
DECLARE signature text;definition text;old_call text;
BEGIN
 old_call:='photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)';
 FOREACH signature IN ARRAY ARRAY[
 'photo.read_v29_negative_dependency_inventory(uuid,uuid,uuid,uuid,uuid,integer,integer)',
 'photo.record_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer)',
 'photo.read_v29_negative_declined_decisions(uuid,uuid,uuid,uuid,uuid,integer)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF strpos(definition,old_call)=0 THEN RAISE EXCEPTION 'NEGATIVE_SERVICE_AUTHORITY_PATCH_MISMATCH';END IF;
  EXECUTE replace(definition,old_call,'photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session)');
 END LOOP;
END $$;

-- UUID cursor is only within a sweep; every new sweep starts from NULL.
CREATE FUNCTION photo.read_v29_negative_scheduler_decisions(p_after uuid,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE result jsonb;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RETURN NULL;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('tenant_id',d.tenant_id,'project_id',d.project_id,
 'decision',jsonb_build_object('review_id',d.id,'origin_task_id',d.task_id,'origin_run_id',d.run_id,
 'decision_hash',d.decision_hash,'route',d.decision_route)) ORDER BY d.id),'[]'::jsonb)
 INTO result FROM (
 SELECT v.tenant_id,w.project_id,v.id,v.task_id,v.run_id,v.decision_hash,v.decision_route
 FROM photo.reviews v JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
 JOIN photo.tenants t ON t.id=v.tenant_id AND t.status='active'
 JOIN photo.projects p ON p.tenant_id=v.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 WHERE v.kind='optical.negative' AND v.state='decided' AND v.revision=2 AND (p_after IS NULL OR v.id>p_after)
 AND ((v.decision='reject' AND v.decision_route='base') OR (v.decision='skip' AND v.decision_route='skip'))
 ORDER BY v.id LIMIT p_limit) d;
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_scheduler_decisions(uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_scheduler_decisions(uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_scheduler_decisions(uuid,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
