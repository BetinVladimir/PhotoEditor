-- +goose Up
-- Resolve a decided V28 source for a future Apply or replacement. This is a
-- versioned locator only; callers must read the exact bytes and recheck it.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_restoration_selected_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,
 p_task uuid,p_run uuid,p_hash text,p_route text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record;locator jsonb;authorized boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_project IS NULL OR p_actor IS NULL OR p_session IS NULL OR p_review IS NULL
  OR p_task IS NULL OR p_run IS NULL OR p_hash IS NULL
  OR p_hash !~ '^[0-9a-f]{64}$' OR p_route NOT IN('candidate','base','skip')
 THEN RETURN NULL;END IF;
 SELECT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
  JOIN photo.projects project ON project.tenant_id=p_tenant
   AND project.id=p_project AND project.archived_at IS NULL
  WHERE authority.role IN('owner','admin','editor') AND
   (authority.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
    SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
     AND acl.project_id=p_project AND acl.user_id=p_actor
     AND acl.role IN('owner','admin','editor')))) INTO authorized;
 IF NOT authorized THEN RETURN NULL;END IF;
 SELECT s.* INTO selected FROM photo.v28_restoration_review_selection(p_tenant,p_task) s
  JOIN photo.reviews v ON v.tenant_id=p_tenant AND v.id=s.review_id
   AND v.run_id=p_run AND v.task_id=p_task
  JOIN photo.v2_workflows workflow ON workflow.tenant_id=p_tenant
   AND workflow.run_id=v.run_id AND workflow.project_id=p_project
  WHERE s.review_id=p_review AND s.decision_hash=p_hash AND s.route=p_route;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p_route<>'skip' THEN
  SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,
   'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,
   'bytes',a.bytes,'media_type',a.media_type) INTO locator
  FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=selected.image_id
   AND a.sha256=selected.image_sha AND a.state='ready'
   AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
   AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  IF locator IS NULL THEN RETURN NULL;END IF;
 ELSIF selected.image_id IS NOT NULL OR selected.image_sha IS NOT NULL
 THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'tenant_id',p_tenant,
  'project_id',p_project,'review_id',p_review,'origin_task_id',p_task,
  'origin_run_id',p_run,'decision_hash',p_hash,'route',p_route,'source',locator);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v28_restoration_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_restoration_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.read_v28_restoration_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
