-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.attach_v30_export_snapshot(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_key text,p_snapshot uuid,p_artifact uuid,p_request_sha text,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_export_snapshot_documents%ROWTYPE;s photo.snapshots%ROWTYPE;body jsonb;node jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_export_snapshot_documents x WHERE x.tenant_id=p_tenant AND x.project_id=p_project AND x.actor_id=p_actor AND x.key=p_key FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' OR d.snapshot_id IS DISTINCT FROM p_snapshot OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.request_sha IS DISTINCT FROM p_request_sha OR d.sha256 IS DISTINCT FROM p_sha
 OR photo.v30_export_snapshot_scope(p_tenant,p_actor,p_session,p_project,p_snapshot,p_artifact,d.request_body,d.document_body,p_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=d.document_body::jsonb;
 -- Lock exact source versions and their assets/objects before repeating authority.
 PERFORM v.id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.master_artifact_id
 WHERE v.tenant_id=p_tenant AND v.id IN(SELECT (x#>>'{binding,asset_version_id}')::uuid FROM jsonb_array_elements(body->'items') x)
 ORDER BY v.id FOR SHARE OF v,a,m;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
 SELECT (x#>>'{recipe,artifact_id}')::uuid FROM jsonb_array_elements(body->'items') x UNION ALL SELECT p_artifact) ORDER BY a.id FOR SHARE;
 IF photo.v30_export_snapshot_scope(p_tenant,p_actor,p_session,p_project,p_snapshot,p_artifact,d.request_body,d.document_body,p_sha) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_snapshot||'/documents/'||p_artifact AND a.object_version=d.object_version
 AND a.media_type='application/json' AND a.output_role='v30_export_snapshot' AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=p_snapshot FOR UPDATE;
 IF FOUND THEN
  IF s.kind IS DISTINCT FROM 'v2' OR s.principal_type IS DISTINCT FROM 'user' OR s.principal_id IS DISTINCT FROM p_actor
  OR s.artifact_id IS DISTINCT FROM p_artifact OR s.digest IS DISTINCT FROM body->>'snapshot_digest'
  OR s.item_count IS DISTINCT FROM jsonb_array_length(body->'items')::bigint OR s.expires_at IS NOT NULL
  OR (SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=p_snapshot)<>s.item_count
  OR EXISTS(SELECT 1 FROM jsonb_array_elements(body->'items') x WHERE NOT EXISTS(
   SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_snapshot AND i.ordinal=(x->>'ordinal')::bigint
   AND i.asset_version_id=(x#>>'{binding,asset_version_id}')::uuid AND i.artifact_id=(x#>>'{binding,source,artifact_id}')::uuid
   AND i.sha256=x#>>'{binding,source,sha256}' AND i.payload=x)) THEN RETURN false;END IF;
  RETURN photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session);
 END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id,expires_at)
 VALUES(p_snapshot,p_tenant,'v2','user',p_actor,body->>'snapshot_digest',jsonb_array_length(body->'items'),p_artifact,NULL);
 FOR node IN SELECT value FROM jsonb_array_elements(body->'items') LOOP
  INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload)
  VALUES(p_tenant,p_snapshot,(node->>'ordinal')::bigint,(node#>>'{binding,asset_version_id}')::uuid,(node#>>'{binding,source,artifact_id}')::uuid,node#>>'{binding,source,sha256}',node);
 END LOOP;
 IF photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'EXPORT_SNAPSHOT_ATTACHMENT_STALE' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.attach_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.attach_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.attach_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
