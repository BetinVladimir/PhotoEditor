-- Exact-version physical reads for the scoped reviewed-defect publisher.
-- Protection is admitted only when declared in the completed repair task.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_defect_artifact(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_review uuid,
 p_role text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,
 sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_row record; allowed boolean:=false;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_review IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_role IS NULL OR
  p_role NOT IN ('image','proposal','labels','selection','resolved',
   'params','protection') THEN RETURN; END IF;
 SELECT * INTO source_row FROM photo.v23_reviewed_defect_source(
  p_tenant,p_project,p_actor,p_auth_session,p_review);
 IF NOT FOUND THEN RETURN; END IF;
 allowed:=CASE p_role
  WHEN 'image' THEN source_row.image_id=p_artifact AND source_row.image_sha=p_sha
  WHEN 'proposal' THEN source_row.proposal_id=p_artifact AND source_row.proposal_sha=p_sha
  WHEN 'labels' THEN source_row.labels_id=p_artifact AND source_row.labels_sha=p_sha
  WHEN 'selection' THEN source_row.selection_id=p_artifact AND source_row.selection_sha=p_sha
  WHEN 'resolved' THEN source_row.resolved_id=p_artifact AND source_row.resolved_sha=p_sha
  WHEN 'params' THEN source_row.params_id=p_artifact AND source_row.params_sha=p_sha
  ELSE EXISTS(SELECT 1 FROM photo.tasks repair
   WHERE repair.tenant_id=p_tenant AND repair.id=source_row.repair_task_id
    AND repair.run_id=source_row.run_id AND repair.state='succeeded'
    AND repair.operation='defects.repair'
    AND repair.inputs @> jsonb_build_array(jsonb_build_object(
     'artifact_id',p_artifact::text,'sha256',p_sha)))
 END;
 IF NOT allowed THEN RETURN; END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,
  a.sha256,a.bytes,a.media_type FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND a.bytes BETWEEN 2 AND CASE p_role
    WHEN 'labels' THEN 401048576
    WHEN 'protection' THEN 801048576
    ELSE 1048576 END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_defect_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_defect_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_reviewed_defect_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
