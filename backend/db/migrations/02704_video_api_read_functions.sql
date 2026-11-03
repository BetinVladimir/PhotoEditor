-- +goose Up
-- photo_api has no table privileges on the video tables (their only RLS policy is trusted_functions), so the HTTP API
-- reads and creates through these SECURITY DEFINER functions. The caller passes a tenant it has already authorized
-- (membership + project ACL) in a tenant-scoped transaction, as for every other video function.

-- +goose StatementBegin
CREATE FUNCTION photo.create_video_sequence(p_tenant uuid,p_project uuid,p_user uuid,p_name text,p_fps_num integer,p_fps_den integer,
 p_width integer,p_height integer) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sid uuid;
BEGIN
 IF p_name IS NULL OR length(btrim(p_name)) NOT BETWEEN 1 AND 120 OR p_width IS NULL OR p_height IS NULL OR p_width%2<>0 OR p_height%2<>0
  OR p_width NOT BETWEEN 16 AND 7680 OR p_height NOT BETWEEN 16 AND 4320
  OR (p_fps_num::text||'/'||p_fps_den::text) NOT IN('24/1','25/1','30/1','50/1','60/1','24000/1001','30000/1001','60000/1001') THEN
  RAISE EXCEPTION 'VIDEO_SEQUENCE_INVALID' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'PROJECT_NOT_FOUND' USING ERRCODE='P0002';END IF;
 INSERT INTO photo.video_sequences(tenant_id,project_id,name,fps_num,fps_den,width,height,created_by)
  VALUES(p_tenant,p_project,btrim(p_name),p_fps_num,p_fps_den,p_width,p_height,p_user) RETURNING id INTO sid;
 RETURN sid;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.get_video_upload(p_tenant uuid,p_upload uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',s.id,'project_id',s.project_id,'principal_id',s.principal_id,'state',s.state,'media_kind',s.media_kind,
  'filename',s.filename,'bytes',s.declared_bytes,'sha256',s.declared_sha256,'object_key',s.object_key,'part_bytes',s.part_bytes,'parts_total',s.parts_total)
 FROM photo.video_upload_sessions s WHERE s.tenant_id=p_tenant AND s.id=p_upload
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.list_video_media(p_tenant uuid,p_project uuid,p_limit integer) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_agg(x ORDER BY x->>'created_at' DESC),'[]'::jsonb) FROM (
  SELECT jsonb_build_object('id',m.id,'kind',m.kind,'name',m.name,'state',m.state,'reject_code',m.reject_code,'probe',m.probe,
   'bytes',m.source_bytes,'source_sha256',m.source_sha256,'created_at',m.created_at,'has_proxy',m.proxy_artifact_id IS NOT NULL,'has_filmstrip',m.filmstrip_artifact_id IS NOT NULL,
   'has_waveform',m.waveform_artifact_id IS NOT NULL) AS x
  FROM photo.video_media m WHERE m.tenant_id=p_tenant AND m.project_id=p_project AND m.withdrawn_at IS NULL
  ORDER BY m.created_at DESC LIMIT least(greatest(coalesce(p_limit,50),1),100)) q
$$;
-- +goose StatementEnd

-- Object of a media derivative: which = proxy | filmstrip | waveform | probe | source.
-- +goose StatementBegin
CREATE FUNCTION photo.get_video_media_object(p_tenant uuid,p_media uuid,p_which text) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'media_type',a.media_type,'bytes',a.bytes,'sha256',a.sha256)
 FROM photo.video_media m JOIN photo.artifacts a ON a.tenant_id=m.tenant_id AND a.id=CASE p_which
   WHEN 'proxy' THEN m.proxy_artifact_id WHEN 'filmstrip' THEN m.filmstrip_artifact_id WHEN 'waveform' THEN m.waveform_artifact_id
   WHEN 'probe' THEN m.probe_artifact_id WHEN 'source' THEN m.source_artifact_id END
 WHERE m.tenant_id=p_tenant AND m.id=p_media AND m.state='ready' AND m.withdrawn_at IS NULL
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.get_video_sequence(p_tenant uuid,p_sequence uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',s.id,'project_id',s.project_id,'name',s.name,'fps_num',s.fps_num,'fps_den',s.fps_den,'width',s.width,'height',s.height,
  'head_revision',s.head_revision,'document',(SELECT jsonb_build_object('revision',v.revision,'sha256',v.document_sha256,'bucket',a.bucket,'object_key',a.object_key,'bytes',a.bytes)
    FROM photo.video_sequence_revisions v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.document_artifact_id
    WHERE v.tenant_id=s.tenant_id AND v.sequence_id=s.id AND v.revision=s.head_revision))
 FROM photo.video_sequences s WHERE s.tenant_id=p_tenant AND s.id=p_sequence AND s.archived_at IS NULL
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.get_video_render(p_tenant uuid,p_render uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',r.id,'project_id',r.project_id,'sequence_id',r.sequence_id,'sequence_revision',r.sequence_revision,'state',r.state,
  'job_state',j.state,'stage',j.stage,'progress',j.progress,'error_code',j.error_code,'duration_ticks',r.duration_ticks,'width',r.width,'height',r.height,
  'encoder',r.encoder,'backend_used',r.backend_used,'output_bytes',r.output_bytes,
  'output',(SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'media_type',a.media_type,'bytes',a.bytes) FROM photo.artifacts a
    WHERE a.tenant_id=r.tenant_id AND a.id=r.output_artifact_id))
 FROM photo.video_renders r LEFT JOIN photo.video_jobs j ON j.tenant_id=r.tenant_id AND j.id=r.job_id
 WHERE r.tenant_id=p_tenant AND r.id=p_render AND r.deleted_at IS NULL
$$;
-- +goose StatementEnd

ALTER FUNCTION photo.create_video_sequence(uuid,uuid,uuid,text,integer,integer,integer,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_upload(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.list_video_media(uuid,uuid,integer) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_media_object(uuid,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_sequence(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.get_video_render(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_video_sequence(uuid,uuid,uuid,text,integer,integer,integer,integer),photo.get_video_upload(uuid,uuid),
 photo.list_video_media(uuid,uuid,integer),photo.get_video_media_object(uuid,uuid,text),photo.get_video_sequence(uuid,uuid),photo.get_video_render(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_video_sequence(uuid,uuid,uuid,text,integer,integer,integer,integer),photo.get_video_upload(uuid,uuid),
 photo.list_video_media(uuid,uuid,integer),photo.get_video_media_object(uuid,uuid,text),photo.get_video_sequence(uuid,uuid),photo.get_video_render(uuid,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
