-- Support A19 blind download without exposing catalog or item metadata.
-- Download-only sessions may freeze only scope=all; selected/filter retain view authority.
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.get_portal_collection(p_session_hash bytea)
RETURNS TABLE(name text,description text,revision bigint,cover jsonb,capabilities jsonb,allowed_presets jsonb,sections jsonb,empty boolean)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;c photo.collections%ROWTYPE;can_view boolean;
BEGIN
 IF octet_length(p_session_hash)<>32 THEN RETURN;END IF;
 SELECT * INTO s FROM photo.share_sessions WHERE session_hash=p_session_hash;
 IF NOT FOUND OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() THEN RETURN;END IF;
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=s.tenant_id AND id=s.share_id AND collection_id=s.collection_id;
 IF NOT FOUND OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version THEN RETURN;END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=s.tenant_id AND id=s.collection_id AND deleted_at IS NULL AND state<>'archived';
 IF NOT FOUND THEN RETURN;END IF;
 can_view:=c.state='published' AND l.permissions->'view'='true'::jsonb;
 RETURN QUERY SELECT c.name,c.description,c.revision,
  CASE WHEN can_view AND c.cover_item_id IS NOT NULL AND EXISTS(SELECT FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published' WHERE i.tenant_id=c.tenant_id AND i.id=c.cover_item_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted') THEN jsonb_build_object('item_id',c.cover_item_id) ELSE NULL::jsonb END,
  jsonb_build_object('view',can_view,'download',l.permissions->'download'='true'::jsonb,'upload',l.permissions->'upload'='true'::jsonb),
  l.allowed_presets,
  CASE WHEN can_view THEN coalesce((SELECT jsonb_agg(jsonb_build_object('id',x.id,'name',x.name,'position',x.position,'revision',x.revision) ORDER BY x.position,x.id) FROM photo.sections x WHERE x.tenant_id=c.tenant_id AND x.collection_id=c.id),'[]'::jsonb) ELSE '[]'::jsonb END,
  NOT can_view OR NOT EXISTS(SELECT FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published' WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted');
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.create_portal_selection(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,p_snapshot uuid,
 p_collection_revision bigint,p_scope text,p_item_ids uuid[],p_section uuid,p_expires timestamptz)
RETURNS TABLE(outcome text,snapshot_id uuid,digest text,item_count bigint,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;c photo.collections%ROWTYPE;idem photo.idempotency_records%ROWTYPE;
 count_value bigint;digest_value text;until_time timestamptz;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$' OR p_request_hash !~ '^[0-9a-f]{64}$'
 OR p_snapshot IS NULL OR p_collection_revision NOT BETWEEN 1 AND 9007199254740991 OR p_scope NOT IN ('selected','all','filter')
 OR (p_scope='selected' AND (cardinality(p_item_ids) NOT BETWEEN 1 AND 5000 OR p_section IS NOT NULL))
 OR (p_scope='all' AND (p_item_ids IS NOT NULL OR p_section IS NOT NULL))
 OR (p_scope='filter' AND (p_item_ids IS NOT NULL OR p_section IS NULL)) OR p_expires IS NULL OR p_expires<=clock_timestamp() OR p_expires>clock_timestamp()+interval '24 hours'
 OR (p_item_ids IS NOT NULL AND ((SELECT count(*) FROM unnest(p_item_ids) x WHERE x IS NULL)>0 OR cardinality(p_item_ids)<>(SELECT count(DISTINCT x) FROM unnest(p_item_ids) x))) THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,CASE WHEN p_scope='all' THEN 'download' ELSE 'view' END);
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.session_id FOR UPDATE;
 IF l.id IS NULL OR s.id IS NULL OR s.session_hash<>p_session_hash OR s.csrf_hash<>p_csrf_hash OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp()
 OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version OR (p_scope='all' AND l.permissions->'download'<>'true'::jsonb) OR (p_scope<>'all' AND l.permissions->'view'<>'true'::jsonb) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(s.tenant_id,'guest',s.id,'Portal.Selection',p_key,p_request_hash,clock_timestamp()+interval '30 days')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=s.tenant_id AND x.principal_type='guest' AND x.principal_id=s.id AND x.route='Portal.Selection' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  RETURN QUERY SELECT 'replayed',snap.id,snap.digest,snap.item_count,snap.expires_at FROM photo.snapshots snap WHERE snap.tenant_id=s.tenant_id AND snap.id=idem.resource_id AND snap.principal_type='guest' AND snap.principal_id=s.id;RETURN;
 END IF;
 SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=s.tenant_id AND x.id=s.collection_id AND x.state='published' AND x.deleted_at IS NULL FOR SHARE;
 IF NOT FOUND OR c.revision<>p_collection_revision OR (p_section IS NOT NULL AND NOT EXISTS(SELECT FROM photo.sections sec WHERE sec.tenant_id=c.tenant_id AND sec.collection_id=c.id AND sec.id=p_section)) THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 WITH eligible AS (
  SELECT i.id,i.asset_version_id,v.master_artifact_id,v.content_hash,i.position
  FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published'
  JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id AND v.review_state='accepted'
  JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
  JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
  WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted'
  AND (p_scope<>'selected' OR i.id=ANY(p_item_ids)) AND (p_scope<>'filter' OR i.section_id=p_section)
 ) SELECT count(*),encode(public.digest(string_agg(id::text||':'||asset_version_id::text||':'||content_hash,',' ORDER BY position,id),'sha256'),'hex')
 INTO count_value,digest_value FROM eligible;
 IF count_value<1 OR count_value>5000 OR (p_scope='selected' AND count_value<>cardinality(p_item_ids)) THEN
  RETURN QUERY SELECT CASE WHEN count_value=0 THEN 'empty' ELSE 'invalid' END,NULL::uuid,NULL::text,NULL::bigint,NULL::timestamptz;RETURN;
 END IF;
 until_time:=least(p_expires,s.expires_at,coalesce(l.expires_at,p_expires));
 INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(p_snapshot,s.tenant_id,'selection',c.id,c.revision,'guest',s.id,digest_value,count_value,until_time);
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
 SELECT s.tenant_id,p_snapshot,row_number() OVER(ORDER BY i.position,i.id)-1,i.id,i.asset_version_id,v.master_artifact_id,v.content_hash,jsonb_build_object('position',i.position)
 FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published'
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id AND v.review_state='accepted'
 JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.deleted_at IS NULL AND a.upload_state='verified'
 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
 WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted'
 AND (p_scope<>'selected' OR i.id=ANY(p_item_ids)) AND (p_scope<>'filter' OR i.section_id=p_section) ORDER BY i.position,i.id;
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('id',p_snapshot,'digest',digest_value,'item_count',count_value,'expires_at',until_time),resource_id=p_snapshot WHERE tenant_id=s.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_snapshot,digest_value,count_value,until_time;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.start_portal_download(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,
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
 cfg_hash:=encode(public.digest(cfg::text,'sha256'),'hex');
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by)
 VALUES(p_run,s.tenant_id,'download','queued',snap.id,cfg,cfg_hash,creator);
 INSERT INTO photo.download_jobs(id,tenant_id,collection_id,share_id,principal_type,principal_id,policy_version,snapshot_id,preset,status,run_id)
 VALUES(p_job,s.tenant_id,s.collection_id,s.share_id,'guest',s.id,s.link_version,snap.id,jsonb_build_object('id',pr.id,'revision',pr.revision,'hash',pr.config_hash,'config',pr.config),'queued',p_run);
 UPDATE photo.idempotency_records SET response_status=202,response_body=jsonb_build_object('download_job_id',p_job,'status','queued'),resource_id=p_job
 WHERE tenant_id=s.tenant_id AND id=idem.id;
 RETURN QUERY SELECT 'created',p_job,'queued';
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.create_tenant(p_user uuid,p_session uuid,p_tenant uuid,p_name text,p_quotas jsonb,p_retention jsonb,p_key text,p_request_hash text)
RETURNS TABLE(outcome text,tenant_id uuid,name text,revision bigint,quotas jsonb,retention jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.auth_idempotency_records%ROWTYPE;t photo.tenants%ROWTYPE;scope bytea;
BEGIN
 IF p_user IS NULL OR p_session IS NULL OR p_tenant IS NULL OR p_name IS NULL OR length(p_name) NOT BETWEEN 1 AND 120
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$' OR p_request_hash !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_quotas)<>'object' OR jsonb_typeof(p_retention)<>'object' THEN
  RAISE EXCEPTION 'INVALID_TENANT_CREATE' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT FROM photo.sessions s JOIN photo.users u ON u.id=s.user_id
  WHERE s.id=p_session AND s.user_id=p_user AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.status='active') THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::text,NULL::bigint,NULL::jsonb,NULL::jsonb;RETURN;END IF;
 scope:=public.digest('tenant-create:'||p_user::text,'sha256');
 INSERT INTO photo.auth_idempotency_records(scope_hash,key,request_hash,response,expires_at)
 VALUES(scope,p_key,p_request_hash,'{}',clock_timestamp()+interval '30 days') ON CONFLICT(scope_hash,key) DO NOTHING;
 SELECT * INTO idem FROM photo.auth_idempotency_records x WHERE x.scope_hash=scope AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN
  RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text,NULL::bigint,NULL::jsonb,NULL::jsonb;RETURN;END IF;
 IF idem.response<>'{}'::jsonb THEN
  RETURN QUERY SELECT 'replayed',(idem.response->>'id')::uuid,idem.response->>'name',(idem.response->>'revision')::bigint,idem.response->'quotas',idem.response->'retention';RETURN;END IF;
 INSERT INTO photo.tenants(id,name,owner_user_id,quotas,retention) VALUES(p_tenant,p_name,p_user,p_quotas,p_retention) RETURNING * INTO t;
 INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(t.id,p_user,'owner','active');
 INSERT INTO photo.tenant_usage(tenant_id) VALUES(t.id);
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(t.id,'user',p_user,'tenant.create','tenant',t.id,gen_random_uuid(),jsonb_build_object('name',t.name));
 UPDATE photo.auth_idempotency_records SET response=jsonb_build_object('id',t.id,'name',t.name,'revision',t.revision,'quotas',t.quotas,'retention',t.retention) WHERE id=idem.id;
 RETURN QUERY SELECT 'created',t.id,t.name,t.revision,t.quotas,t.retention;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.begin_portal_upload(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,
 p_upload uuid,p_reservation uuid,p_object_key text,p_filename text,p_bytes bigint,p_format text,p_media_type text,p_assume_profile text,p_expected_sha text,p_expires timestamptz)
RETURNS TABLE(outcome text,id uuid,project_id uuid,collection_id uuid,state text,revision bigint,filename text,expected_size bigint,
 actual_size bigint,expires_at timestamptz,object_key text,multipart_id text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sh photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;c photo.collections%ROWTYPE;u photo.upload_sessions%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE;storage bigint;reserved bigint;files bigint;reserved_files bigint;byte_limit numeric;file_limit numeric;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$'
 OR p_request_hash !~ '^[0-9a-f]{64}$' OR p_upload IS NULL OR p_reservation IS NULL OR p_upload=p_reservation
 OR p_object_key IS NULL OR length(p_object_key) NOT BETWEEN 1 AND 1024 OR p_filename IS NULL OR length(p_filename) NOT BETWEEN 1 AND 255
 OR p_bytes NOT BETWEEN 1 AND 536870912 OR p_format !~ '^[a-z0-9]{2,8}$' OR p_media_type !~ '^[a-z0-9.+-]+/[a-z0-9.+-]+$'
 OR (p_assume_profile IS NOT NULL AND p_assume_profile NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
 OR (p_expected_sha IS NOT NULL AND p_expected_sha !~ '^[0-9a-f]{64}$') OR p_expires IS NULL OR p_expires<=clock_timestamp()
 OR p_expires>clock_timestamp()+interval '24 hours' THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 SELECT * INTO sh FROM photo.share_sessions WHERE session_hash=p_session_hash;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 SELECT tu.storage_bytes,tu.reserved_bytes,tu.source_files,tu.reserved_files,(t.quotas->>'storage_bytes')::numeric,(t.quotas->>'source_files')::numeric
 INTO storage,reserved,files,reserved_files,byte_limit,file_limit FROM photo.tenant_usage tu JOIN photo.tenants t ON t.id=tu.tenant_id WHERE tu.tenant_id=sh.tenant_id FOR UPDATE OF tu;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=sh.tenant_id AND x.id=sh.share_id FOR UPDATE;
 SELECT * INTO sh FROM photo.share_sessions x WHERE x.tenant_id=sh.tenant_id AND x.id=sh.id FOR UPDATE;
 SELECT * INTO c FROM photo.collections x WHERE x.tenant_id=sh.tenant_id AND x.id=sh.collection_id AND x.deleted_at IS NULL FOR UPDATE;
 IF l.id IS NULL OR c.id IS NULL OR sh.csrf_hash<>p_csrf_hash OR sh.revoked_at IS NOT NULL OR sh.expires_at<=clock_timestamp()
 OR l.collection_id<>sh.collection_id OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
 OR l.policy_version<>sh.link_version OR l.permissions->'upload'<>'true'::jsonb OR c.state='archived'
 OR jsonb_typeof(l.limits)<>'object' OR jsonb_typeof(l.limits->'max_upload_files')<>'number'
 OR jsonb_typeof(l.limits->'max_upload_bytes_total')<>'number' OR jsonb_typeof(l.limits->'max_file_bytes')<>'number'
 OR jsonb_typeof(l.limits->'allowed_formats')<>'array' OR NOT (l.limits->'allowed_formats' ? p_format) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(sh.tenant_id,'guest',sh.id,'Upload.BeginGuest',p_key,p_request_hash,least(sh.expires_at,clock_timestamp()+interval '30 days'))
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=sh.tenant_id AND x.principal_type='guest' AND x.principal_id=sh.id AND x.route='Upload.BeginGuest' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=sh.tenant_id AND x.id=idem.resource_id AND x.principal_type='guest' AND x.principal_id=sh.id AND x.share_id=l.id;
  IF NOT FOUND THEN RAISE EXCEPTION 'PORTAL_UPLOAD_REPLAY_MISSING' USING ERRCODE='23514';END IF;
  RETURN QUERY SELECT 'replayed',u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;RETURN;
 END IF;
 IF p_bytes>(l.limits->>'max_file_bytes')::numeric OR l.used_bytes::numeric+l.reserved_bytes+p_bytes>(l.limits->>'max_upload_bytes_total')::numeric
 OR l.used_files::numeric+l.reserved_files+1>(l.limits->>'max_upload_files')::numeric
 OR storage::numeric+reserved+p_bytes>byte_limit OR files::numeric+reserved_files+1>file_limit
 OR storage::numeric+reserved+p_bytes>9007199254740991 OR files::numeric+reserved_files+1>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,share_id,policy_version,expected_size,filename,assume_profile,object_key,state,expires_at,reservation_id,media_type,expected_sha256)
 VALUES(p_upload,sh.tenant_id,c.project_id,c.id,'guest',sh.id,l.id,l.policy_version,p_bytes,p_filename,p_assume_profile,p_object_key,'initializing',least(p_expires,sh.expires_at),p_reservation,p_media_type,p_expected_sha);
 INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,share_id,holder_type,holder_id,bytes,files,state,expires_at)
 VALUES(p_reservation,sh.tenant_id,'guest',sh.id,l.id,'upload',p_upload,p_bytes,1,'reserved',least(p_expires,sh.expires_at));
 UPDATE photo.tenant_usage AS target SET reserved_bytes=target.reserved_bytes+p_bytes,reserved_files=target.reserved_files+1 WHERE target.tenant_id=sh.tenant_id;
 UPDATE photo.share_links AS target SET reserved_bytes=target.reserved_bytes+p_bytes,reserved_files=target.reserved_files+1 WHERE target.tenant_id=sh.tenant_id AND target.id=l.id;
 UPDATE photo.idempotency_records AS target SET response_status=201,response_body=jsonb_build_object('upload_id',p_upload),resource_id=p_upload WHERE target.tenant_id=sh.tenant_id AND target.id=idem.id;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=sh.tenant_id AND upload_sessions.id=p_upload;
 RETURN QUERY SELECT 'created',u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
