-- +goose Up
-- FN-D-12 finish_video_render, FN-D-14 delivery management, FN-D-15 record_video_egress.
-- Render bytes are charged by finish_video_job (FN-D-09); finish_video_render only moves the render row.

-- +goose StatementBegin
CREATE FUNCTION photo.finish_video_render(p_tenant uuid,p_render uuid,p_state text,p_output uuid,p_qc uuid,p_duration_ticks bigint,
 p_encoder text,p_backend text,p_fingerprint text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.video_renders%ROWTYPE;out_bytes bigint;
BEGIN
 IF p_state NOT IN('ready','failed','cancelled') THEN RAISE EXCEPTION 'VIDEO_RENDER_STATE' USING ERRCODE='22023';END IF;
 SELECT * INTO r FROM photo.video_renders WHERE tenant_id=p_tenant AND id=p_render FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF r.state IN('ready','failed','cancelled','deleted') THEN RETURN r.state=p_state;END IF;
 IF p_state<>'ready' THEN
  UPDATE photo.video_renders SET state=p_state,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_render;
  RETURN true;END IF;
 SELECT bytes INTO out_bytes FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_output AND state='ready' AND output_role='video_render';
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_RENDER_OUTPUT_MISSING' USING ERRCODE='55000';END IF;
 IF NOT EXISTS(SELECT FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_qc AND state='ready' AND output_role='video_qc_report') THEN
  RAISE EXCEPTION 'VIDEO_RENDER_QC_MISSING' USING ERRCODE='55000';END IF;
 IF p_fingerprint IS NULL OR p_fingerprint !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'VIDEO_RENDER_FINGERPRINT' USING ERRCODE='22023';END IF;
 UPDATE photo.video_renders SET state='ready',output_artifact_id=p_output,output_bytes=out_bytes,qc_report_artifact_id=p_qc,duration_ticks=p_duration_ticks,
  encoder=p_encoder,backend_used=p_backend,render_fingerprint=p_fingerprint,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_render;
 RETURN true;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.create_video_delivery(p_tenant uuid,p_project uuid,p_name text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE cid uuid;
BEGIN
 IF p_name IS NULL OR length(btrim(p_name)) NOT BETWEEN 1 AND 120 THEN RAISE EXCEPTION 'VIDEO_DELIVERY_NAME' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'PROJECT_NOT_FOUND' USING ERRCODE='P0002';END IF;
 INSERT INTO photo.collections(tenant_id,project_id,name,content_kind) VALUES(p_tenant,p_project,btrim(p_name),'video') RETURNING id INTO cid;
 RETURN cid;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.add_video_render_to_delivery(p_tenant uuid,p_collection uuid,p_render uuid,p_title text) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.collections%ROWTYPE;ord integer;
BEGIN
 IF p_title IS NULL OR length(p_title) NOT BETWEEN 1 AND 200 THEN RAISE EXCEPTION 'VIDEO_DELIVERY_TITLE' USING ERRCODE='22023';END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=p_tenant AND id=p_collection AND content_kind='video' AND deleted_at IS NULL AND state IN('draft','published') FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_DELIVERY_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF NOT EXISTS(SELECT FROM photo.video_renders r WHERE r.tenant_id=p_tenant AND r.project_id=c.project_id AND r.id=p_render AND r.state='ready') THEN
  RAISE EXCEPTION 'VIDEO_RENDER_NOT_READY' USING ERRCODE='55000';END IF;
 SELECT ordinal INTO ord FROM photo.video_collection_items WHERE tenant_id=p_tenant AND collection_id=p_collection AND render_id=p_render;
 IF FOUND THEN RETURN ord;END IF;
 SELECT coalesce(max(ordinal)+1,0) INTO ord FROM photo.video_collection_items WHERE tenant_id=p_tenant AND collection_id=p_collection;
 IF ord>999 THEN RAISE EXCEPTION 'VIDEO_DELIVERY_FULL' USING ERRCODE='53400';END IF;
 INSERT INTO photo.video_collection_items(tenant_id,collection_id,render_id,ordinal,title,published_at)
  VALUES(p_tenant,p_collection,p_render,ord,p_title,CASE WHEN c.state='published' THEN clock_timestamp() END);
 RETURN ord;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.publish_video_delivery(p_tenant uuid,p_collection uuid,p_publish boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE c photo.collections%ROWTYPE;
BEGIN
 SELECT * INTO c FROM photo.collections WHERE tenant_id=p_tenant AND id=p_collection AND content_kind='video' AND deleted_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'VIDEO_DELIVERY_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF p_publish THEN
  IF c.state='published' THEN RETURN true;END IF;
  IF c.state<>'draft' THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT FROM photo.video_collection_items WHERE tenant_id=p_tenant AND collection_id=p_collection) THEN RAISE EXCEPTION 'VIDEO_DELIVERY_EMPTY' USING ERRCODE='55000';END IF;
  UPDATE photo.collections SET state='published',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_collection;
  UPDATE photo.video_collection_items SET published_at=coalesce(published_at,clock_timestamp()),revision=revision+1 WHERE tenant_id=p_tenant AND collection_id=p_collection;
 ELSE
  IF c.state='draft' THEN RETURN true;END IF;
  IF c.state<>'published' THEN RETURN false;END IF;
  UPDATE photo.collections SET state='draft',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_collection;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd

-- The caller derives id and secret_hash exactly as for photo shares (collection/share.go); this only inserts the row.
-- +goose StatementBegin
CREATE FUNCTION photo.create_video_share(p_tenant uuid,p_collection uuid,p_id uuid,p_secret_hash bytea,p_label text,p_download boolean,
 p_expires timestamptz,p_limits jsonb,p_user uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_id IS NULL OR p_secret_hash IS NULL OR octet_length(p_secret_hash)<>32 OR p_label IS NULL OR length(p_label)>120
  OR p_limits IS NULL OR jsonb_typeof(p_limits)<>'object' OR p_expires IS NULL OR p_expires<=clock_timestamp() THEN
  RAISE EXCEPTION 'VIDEO_SHARE_INVALID' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.collections WHERE tenant_id=p_tenant AND id=p_collection AND content_kind='video' AND state='published' AND deleted_at IS NULL) THEN
  RAISE EXCEPTION 'VIDEO_DELIVERY_NOT_PUBLISHED' USING ERRCODE='55000';END IF;
 INSERT INTO photo.share_links(id,tenant_id,collection_id,secret_hash,label,permissions,expires_at,limits,moderation,allowed_presets,created_by)
  VALUES(p_id,p_tenant,p_collection,p_secret_hash,p_label,jsonb_build_object('view',true,'download',coalesce(p_download,false),'upload',false),
  p_expires,p_limits,'owner_review','[]'::jsonb,p_user);
 RETURN p_id;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.revoke_video_share(p_tenant uuid,p_share uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.share_links l SET revoked_at=coalesce(l.revoked_at,clock_timestamp()),policy_version=policy_version+1
  WHERE l.tenant_id=p_tenant AND l.id=p_share AND EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=l.tenant_id AND c.id=l.collection_id AND c.content_kind='video');
 RETURN FOUND;
END $$;
-- +goose StatementEnd

-- Download event (stream traffic is accounted by the lease functions, DS-28). Portal role only.
-- +goose StatementBegin
CREATE FUNCTION photo.record_video_egress(p_tenant uuid,p_share uuid,p_render uuid,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_bytes IS NULL OR p_bytes<0 THEN RAISE EXCEPTION 'VIDEO_EGRESS_BYTES' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.share_links l
  JOIN photo.collections c ON c.tenant_id=l.tenant_id AND c.id=l.collection_id AND c.content_kind='video' AND c.state='published' AND c.deleted_at IS NULL
  JOIN photo.video_collection_items i ON i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.render_id=p_render
  WHERE l.tenant_id=p_tenant AND l.id=p_share AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at>clock_timestamp())
   AND (l.permissions->>'download')::boolean) THEN RETURN false;END IF;
 INSERT INTO photo.video_egress_events(tenant_id,share_id,render_id,kind,bytes) VALUES(p_tenant,p_share,p_render,'download',p_bytes);
 RETURN true;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.finish_video_render(uuid,uuid,text,uuid,uuid,bigint,text,text,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.create_video_delivery(uuid,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.add_video_render_to_delivery(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.publish_video_delivery(uuid,uuid,boolean) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.create_video_share(uuid,uuid,uuid,bytea,text,boolean,timestamptz,jsonb,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.revoke_video_share(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.record_video_egress(uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_video_render(uuid,uuid,text,uuid,uuid,bigint,text,text,text),photo.create_video_delivery(uuid,uuid,text),
 photo.add_video_render_to_delivery(uuid,uuid,uuid,text),photo.publish_video_delivery(uuid,uuid,boolean),
 photo.create_video_share(uuid,uuid,uuid,bytea,text,boolean,timestamptz,jsonb,uuid),photo.revoke_video_share(uuid,uuid),
 photo.record_video_egress(uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_video_render(uuid,uuid,text,uuid,uuid,bigint,text,text,text) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.create_video_delivery(uuid,uuid,text),photo.add_video_render_to_delivery(uuid,uuid,uuid,text),
 photo.publish_video_delivery(uuid,uuid,boolean),photo.create_video_share(uuid,uuid,uuid,bytea,text,boolean,timestamptz,jsonb,uuid),
 photo.revoke_video_share(uuid,uuid) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.record_video_egress(uuid,uuid,uuid,bigint) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
