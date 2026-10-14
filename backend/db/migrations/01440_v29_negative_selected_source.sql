-- +goose Up
-- Metadata only. Original WorkingImage bytes must be read/verified outside SQL,
-- followed by another lookup. Replacement admission retains its own checks.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_selected_source(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_task uuid,p_run uuid,p_hash text,p_route text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record;source jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_route IS NULL OR p_route NOT IN('base','skip') OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$'
 OR photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT s.* INTO selected FROM photo.v29_negative_review_selection(p_tenant,p_task) s
 JOIN photo.reviews v ON v.tenant_id=p_tenant AND v.id=s.review_id AND v.run_id=p_run
 JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id AND w.project_id=p_project
 WHERE s.review_id=p_review AND s.decision_hash=p_hash AND s.route=p_route;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p_route='base' THEN
  SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
   'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) INTO source
  FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=selected.image_id AND a.sha256=selected.image_sha
  AND a.state='ready' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
  IF source IS NULL THEN RETURN NULL;END IF;
 ELSE
  IF selected.image_id IS NOT NULL OR selected.image_sha IS NOT NULL THEN RETURN NULL;END IF;
 END IF;
 RETURN jsonb_build_object('schema_version',1,'tenant_id',p_tenant,'project_id',p_project,
 'review_id',p_review,'origin_task_id',p_task,'origin_run_id',p_run,'decision_hash',p_hash,'route',p_route,'source',source);
END $$;
ALTER FUNCTION photo.read_v29_negative_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
