-- Pending training admission restores only frozen recipes named by its
-- durable recovery manifest; no current candidate inventory is consulted.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_restore(p_tenant uuid,p_run uuid,
 p_profile uuid,p_event uuid,p_side text,p_artifact uuid,p_sha text,
 p_actor uuid,p_auth_session uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,
 bytes bigint,media_type text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_profile IS NULL OR
  p_event IS NULL OR p_artifact IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_side NOT IN ('base','approved') OR
  coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V23_TRAIN_RESTORE_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run;
 IF r.id IS NULL OR r.created_by<>p_actor OR r.status<>'draft' OR
  r.graph_sealed OR w.id IS NULL OR w.workflow_type<>'learning' OR
  r.config_snapshot->'v23_training'->>'profile_id'<>p_profile::text OR
  NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_auth_session)
  OR NOT photo.v23_training_frozen_scope(p_tenant,p_run,p_event,p_side,p_sha)
 THEN RAISE EXCEPTION 'V23_TRAIN_RESTORE_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.backend_documents d
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  AND a.sha256=d.sha256 AND a.object_version=d.object_version
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_event
  AND d.purpose='v23_frozen_'||p_side AND d.artifact_id=p_artifact
  AND d.sha256=p_sha AND d.state='ready' AND a.state='ready'
  AND a.output_role='v23_frozen_recipe' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 1048576 AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_restore(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_restore(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_frozen_restore(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
