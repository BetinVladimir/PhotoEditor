-- Return only the exact immutable JSON objects of a currently linked
-- correction-event portrait.detail producer. Call before and after object IO.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_portrait_detail_artifact(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,
 p_event uuid,p_role text,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,
 sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_row record; wanted uuid; wanted_sha text;
 a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_role NOT IN ('image','params','resolved') THEN RETURN;END IF;
 SELECT * INTO source_row FROM photo.v23_portrait_detail_event_link_source(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND THEN RETURN;END IF;
 CASE p_role
  WHEN 'image' THEN wanted:=source_row.image_id;
   wanted_sha:=source_row.image_sha;
  WHEN 'params' THEN wanted:=source_row.params_id;
   wanted_sha:=source_row.params_sha;
  WHEN 'resolved' THEN wanted:=source_row.resolved_id;
   wanted_sha:=source_row.resolved_sha;
 END CASE;
 IF wanted IS DISTINCT FROM p_artifact OR
  wanted_sha IS DISTINCT FROM p_sha THEN RETURN;END IF;
 SELECT * INTO a FROM photo.artifacts x WHERE x.tenant_id=p_tenant AND
  x.id=p_artifact AND x.sha256=p_sha AND x.state='ready' AND
  x.media_type='application/json' AND x.verified_at IS NOT NULL AND
  x.object_version IS NOT NULL AND x.object_version NOT IN ('','null') AND
  x.bytes BETWEEN 2 AND 1048576 AND
  (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,
  a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_portrait_detail_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_portrait_detail_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_portrait_detail_artifact(
 uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
