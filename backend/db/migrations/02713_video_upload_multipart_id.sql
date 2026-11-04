-- +goose Up
-- The API signs parts of an upload later than it starts it, so get_video_upload now returns the multipart id too.
CREATE OR REPLACE FUNCTION photo.get_video_upload(p_tenant uuid,p_upload uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',s.id,'project_id',s.project_id,'principal_id',s.principal_id,'state',s.state,'media_kind',s.media_kind,
  'filename',s.filename,'bytes',s.declared_bytes,'sha256',s.declared_sha256,'object_key',s.object_key,'part_bytes',s.part_bytes,'parts_total',s.parts_total,
  'multipart_id',s.multipart_id)
 FROM photo.video_upload_sessions s WHERE s.tenant_id=p_tenant AND s.id=p_upload
$$;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
