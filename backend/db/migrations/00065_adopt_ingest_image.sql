-- +goose Up
ALTER TABLE photo.ingest_catalog_items ADD COLUMN settled_at timestamptz;
ALTER TABLE photo.ingest_catalog_items ADD COLUMN staging_bytes bigint CHECK(staging_bytes BETWEEN 1 AND 536870912);
ALTER TABLE photo.ingest_catalog_items ADD CONSTRAINT ingest_settlement_pair CHECK((settled_at IS NULL)=(staging_bytes IS NULL));
GRANT UPDATE(settled_at,staging_bytes) ON photo.ingest_catalog_items TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.adopt_ingest_image(p_tenant uuid,p_run uuid,p_ordinal bigint,p_master uuid,p_sha text,p_image jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.ingest_upload_bindings%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 r photo.runs%ROWTYPE;c photo.collections%ROWTYPE;link photo.share_links%ROWTYPE;receipt photo.ingest_catalog_items%ROWTYPE;
 actual bigint;position_value bigint;moderation text:='pending_review';visible text:='hidden';publish boolean:=false;reserved bigint;
BEGIN
 -- Immutable hint only; all mutable rows are locked in usage→share→run→project→collection→upload→quota order.
 SELECT * INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT reserved_bytes INTO reserved FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=b.upload_id;
 IF NOT FOUND THEN RETURN false;END IF;
 IF u.share_id IS NOT NULL THEN
  SELECT * INTO link FROM photo.share_links WHERE tenant_id=p_tenant AND id=u.share_id FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase IN ('decode','catalog')) THEN RETURN false;END IF;
 SELECT * INTO receipt FROM photo.ingest_catalog_items x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF FOUND AND receipt.settled_at IS NOT NULL THEN
  RETURN receipt.master_id=p_master AND receipt.master_sha=p_sha AND EXISTS(SELECT FROM photo.upload_sessions x WHERE x.tenant_id=p_tenant AND x.id=b.upload_id AND x.state='verified' AND x.verified_version_id=b.version_id);
 END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=b.project_id AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF b.collection_id IS NOT NULL THEN
  SELECT * INTO c FROM photo.collections WHERE tenant_id=p_tenant AND id=b.collection_id AND project_id=b.project_id AND deleted_at IS NULL FOR UPDATE;
  IF NOT FOUND OR c.revision>=9007199254740991 THEN RETURN false;END IF;
 END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=b.upload_id FOR UPDATE;
 IF NOT FOUND OR u.state<>'verifying' OR u.verified_version_id IS NOT NULL OR u.staging_artifact_id IS DISTINCT FROM b.source_id
 OR u.project_id<>b.project_id OR u.collection_id IS DISTINCT FROM b.collection_id OR u.principal_type<>b.principal_type OR u.principal_id<>b.principal_id
 OR u.revision>=9007199254740991 OR u.share_id IS DISTINCT FROM link.id THEN RETURN false;END IF;
 SELECT bytes INTO actual FROM photo.artifacts WHERE tenant_id=p_tenant AND id=b.source_id AND sha256=b.source_sha AND state='ready';
 IF NOT FOUND OR actual IS DISTINCT FROM u.actual_size OR actual NOT BETWEEN 1 AND 536870912 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.principal_type<>u.principal_type OR q.principal_id<>u.principal_id
 OR q.share_id IS DISTINCT FROM u.share_id OR q.files<>1 OR q.bytes<actual OR q.bytes<>u.expected_size OR reserved<q.bytes THEN RETURN false;END IF;
 IF link.id IS NOT NULL AND (link.reserved_bytes<q.bytes OR link.reserved_files<1 OR link.used_bytes>9007199254740991-actual OR link.used_files>=9007199254740991) THEN RETURN false;END IF;
 IF b.principal_type='user' THEN moderation:='accepted';
 ELSIF link.id IS NOT NULL AND link.collection_id=b.collection_id AND link.policy_version=u.policy_version
 AND link.revoked_at IS NULL AND (link.expires_at IS NULL OR link.expires_at>clock_timestamp())
 AND link.permissions->'upload'='true'::jsonb AND link.moderation='auto_after_validation' THEN
  moderation:='accepted';publish:=c.state='published';
 END IF;
 IF publish THEN visible:='visible';END IF;
 -- register_ingest_asset cannot partially mutate on false; any later exception rolls back its inserts too.
 IF NOT photo.register_ingest_asset(p_tenant,p_run,p_ordinal,p_master,p_sha,p_image) THEN RETURN false;END IF;
 IF b.collection_id IS NOT NULL THEN
  SELECT coalesce(max(position),0) INTO position_value FROM photo.collection_items WHERE tenant_id=p_tenant AND collection_id=b.collection_id;
  IF position_value>9007199254739967 THEN
   WITH ranked AS (SELECT id,row_number() OVER(ORDER BY position,id)*1024 AS pos FROM photo.collection_items WHERE tenant_id=p_tenant AND collection_id=b.collection_id)
   UPDATE photo.collection_items item SET position=ranked.pos FROM ranked WHERE item.id=ranked.id AND item.tenant_id=p_tenant;
   SELECT coalesce(max(position),0) INTO position_value FROM photo.collection_items WHERE tenant_id=p_tenant AND collection_id=b.collection_id;
  END IF;
  IF position_value>9007199254739967 THEN RAISE EXCEPTION 'COLLECTION_POSITION_EXHAUSTED' USING ERRCODE='23514';END IF;
  INSERT INTO photo.collection_items(id,tenant_id,collection_id,asset_id,asset_version_id,position,visibility,moderation_state,created_by_run_id)
  VALUES(b.item_id,p_tenant,b.collection_id,b.asset_id,b.version_id,position_value+1024,visible,moderation,p_run);
  UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=p_tenant AND id=b.collection_id;
  IF publish THEN
   INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,published_by,published_at,policy_version)
   VALUES(p_tenant,b.item_id,b.version_id,link.created_by,clock_timestamp(),link.policy_version);
  END IF;
 END IF;
 UPDATE photo.asset_versions SET review_state=moderation WHERE tenant_id=p_tenant AND id=b.version_id;
 -- Original is already charged by SQL61. Convert the source reservation to the
 -- still-existing staging object's actual bytes; later physical cleanup releases it.
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes,storage_bytes=storage_bytes+actual
 WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes AND storage_bytes<=9007199254740991-actual;
 IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 IF link.id IS NOT NULL THEN
  UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-1,used_bytes=used_bytes+actual,used_files=used_files+1
  WHERE tenant_id=p_tenant AND id=link.id;
 END IF;
 UPDATE photo.quota_reservations SET state='consumed',bytes=actual WHERE tenant_id=p_tenant AND id=q.id;
 UPDATE photo.upload_sessions SET state='verified',verified_version_id=b.version_id,revision=revision+1 WHERE tenant_id=p_tenant AND id=u.id;
 UPDATE photo.ingest_catalog_items SET settled_at=clock_timestamp(),staging_bytes=actual WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb) TO photo_scheduler;
-- Only the complete adoption transaction is callable by the scheduler.
REVOKE EXECUTE ON FUNCTION photo.register_ingest_asset(uuid,uuid,bigint,uuid,text,jsonb) FROM photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
