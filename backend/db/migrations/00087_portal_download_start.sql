-- +goose Up
-- Atomically bind a current guest download grant, immutable selection and resolved preset.
-- +goose StatementBegin
CREATE FUNCTION photo.start_portal_download(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,
 p_job uuid,p_run uuid,p_snapshot uuid,p_preset uuid)
RETURNS TABLE(outcome text,job_id uuid,status text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;snap photo.snapshots%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE;pr photo.export_presets%ROWTYPE;creator uuid;cfg jsonb;cfg_hash text;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$'
 OR p_request_hash !~ '^[0-9a-f]{64}$' OR p_job IS NULL OR p_run IS NULL OR p_snapshot IS NULL OR p_preset IS NULL THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::text;RETURN;
 END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'download');
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.session_id FOR UPDATE;
 IF l.id IS NULL OR s.id IS NULL OR s.session_hash<>p_session_hash OR s.csrf_hash<>p_csrf_hash OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp()
 OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version OR l.permissions->'download'<>'true'::jsonb THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(s.tenant_id,'guest',s.id,'Portal.Download.Start',p_key,p_request_hash,clock_timestamp()+interval '30 days')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=s.tenant_id AND x.principal_type='guest' AND x.principal_id=s.id AND x.route='Portal.Download.Start' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  RETURN QUERY SELECT 'replayed',j.id,j.status FROM photo.download_jobs j WHERE j.tenant_id=s.tenant_id AND j.id=idem.resource_id AND j.principal_type='guest' AND j.principal_id=s.id;RETURN;
 END IF;
 SELECT * INTO snap FROM photo.snapshots x WHERE x.tenant_id=s.tenant_id AND x.id=p_snapshot AND x.kind='selection'
  AND x.collection_id=s.collection_id AND x.principal_type='guest' AND x.principal_id=s.id AND x.item_count BETWEEN 1 AND 5000
  AND (x.expires_at IS NULL OR x.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'snapshot_invalid',NULL::uuid,NULL::text;RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.collections c JOIN photo.projects p ON p.tenant_id=c.tenant_id AND p.id=c.project_id AND p.download_allowed
   WHERE c.tenant_id=s.tenant_id AND c.id=s.collection_id AND c.state='published' AND c.deleted_at IS NULL)
 OR EXISTS(SELECT FROM photo.snapshot_items si
   LEFT JOIN photo.collection_items i ON i.tenant_id=si.tenant_id AND i.id=si.item_id AND i.collection_id=s.collection_id AND i.asset_version_id=si.asset_version_id AND i.visibility='visible' AND i.moderation_state='accepted'
   LEFT JOIN photo.publications pub ON pub.tenant_id=si.tenant_id AND pub.item_id=si.item_id AND pub.asset_version_id=si.asset_version_id AND pub.state='published'
   LEFT JOIN photo.asset_versions v ON v.tenant_id=si.tenant_id AND v.id=si.asset_version_id AND v.review_state='accepted' AND v.master_artifact_id=si.artifact_id AND v.content_hash=si.sha256
   LEFT JOIN photo.artifacts a ON a.tenant_id=si.tenant_id AND a.id=si.artifact_id AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   WHERE si.tenant_id=s.tenant_id AND si.snapshot_id=snap.id AND (i.id IS NULL OR pub.id IS NULL OR v.id IS NULL OR a.id IS NULL)) THEN
  RETURN QUERY SELECT 'revoked',NULL::uuid,NULL::text;RETURN;
 END IF;
 SELECT * INTO pr FROM photo.export_presets x WHERE x.tenant_id=s.tenant_id AND x.id=p_preset AND x.archived_at IS NULL FOR SHARE;
 IF NOT FOUND OR jsonb_typeof(l.allowed_presets)<>'array' OR NOT EXISTS(
   SELECT FROM jsonb_array_elements(l.allowed_presets) e
   WHERE (jsonb_typeof(e)='string' AND trim(both '"' from e::text)=p_preset::text)
      OR (jsonb_typeof(e)='object' AND e->>'id'=p_preset::text)) THEN
  RETURN QUERY SELECT 'preset_invalid',NULL::uuid,NULL::text;RETURN;
 END IF;
 SELECT created_by INTO creator FROM photo.share_links WHERE tenant_id=s.tenant_id AND id=s.share_id;
 cfg:=jsonb_build_object('schema_version',1,'pipeline','download','pipeline_version',3,'input_snapshot_id',snap.id,'input_snapshot_digest',snap.digest,
  'preset_id',pr.id,'preset_revision',pr.revision,'preset_hash',pr.config_hash,'preset',pr.config,'share_id',s.share_id,'policy_version',s.link_version,
  'failure_policy','continue_items','scope','selected');
 cfg_hash:=encode(digest(cfg::text,'sha256'),'hex');
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by)
 VALUES(p_run,s.tenant_id,'download','queued',snap.id,cfg,cfg_hash,creator);
 INSERT INTO photo.download_jobs(id,tenant_id,collection_id,share_id,principal_type,principal_id,policy_version,snapshot_id,preset,status,run_id)
 VALUES(p_job,s.tenant_id,s.collection_id,s.share_id,'guest',s.id,s.link_version,snap.id,jsonb_build_object('id',pr.id,'revision',pr.revision,'hash',pr.config_hash,'config',pr.config),'queued',p_run);
 UPDATE photo.idempotency_records SET response_status=202,response_body=jsonb_build_object('download_job_id',p_job,'status','queued'),resource_id=p_job
 WHERE tenant_id=s.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_job,'queued';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
