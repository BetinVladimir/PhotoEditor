-- Freeze the validated selection before V21 draft admission. The caller must
-- use the deterministic run UUID derived from the request idempotency key.
-- The transaction aborts if the selection, session revision or ACL changes.
-- +goose StatementBegin
CREATE FUNCTION photo.freeze_v22_analysis_snapshot(
 p_tenant uuid,p_run uuid,p_session uuid,p_expected bigint,p_selection uuid,
 p_manual jsonb,p_actor uuid,p_auth_session uuid,p_input_digest text)
RETURNS TABLE(snapshot_id uuid,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source photo.snapshots%ROWTYPE; frozen photo.snapshots%ROWTYPE;
 h bytea; target uuid; preflight jsonb; copied bigint;
BEGIN
 IF p_run IS NULL OR p_input_digest IS NULL OR
  p_input_digest !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V22_ANALYSIS_RUN_INVALID' USING ERRCODE='22023';
 END IF;
 preflight:=photo.preflight_v22_analysis(p_tenant,p_session,p_expected,
  p_selection,p_manual,p_actor,p_auth_session);
 SELECT * INTO source FROM photo.snapshots s WHERE s.tenant_id=p_tenant
  AND s.id=p_selection FOR SHARE;
 h:=public.digest(uuid_send(p_run)||convert_to('analysis.snapshot','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 target:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO frozen FROM photo.snapshots s WHERE s.tenant_id=p_tenant
  AND s.id=target FOR UPDATE;
 IF frozen.id IS NOT NULL THEN
  IF frozen.kind<>'v2' OR frozen.collection_id IS DISTINCT FROM source.collection_id
   OR frozen.source_revision IS DISTINCT FROM source.source_revision
   OR frozen.principal_type<>'user' OR frozen.principal_id<>p_actor
   OR frozen.digest<>p_input_digest OR frozen.item_count<>source.item_count
   OR EXISTS(SELECT i.ordinal,i.item_id,i.asset_version_id,i.artifact_id,i.sha256,i.payload
     FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_selection
     EXCEPT SELECT i.ordinal,i.item_id,i.asset_version_id,i.artifact_id,i.sha256,i.payload
     FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=target)
   OR EXISTS(SELECT i.ordinal,i.item_id,i.asset_version_id,i.artifact_id,i.sha256,i.payload
     FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=target
     EXCEPT SELECT i.ordinal,i.item_id,i.asset_version_id,i.artifact_id,i.sha256,i.payload
     FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_selection) THEN
   RAISE EXCEPTION 'V22_ANALYSIS_SNAPSHOT_CONFLICT' USING ERRCODE='23514';
  END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run) THEN
   RAISE EXCEPTION 'V22_ANALYSIS_RUN_ALREADY_ADMITTED' USING ERRCODE='23514';
  END IF;
  INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,
   principal_type,principal_id,digest,item_count,expires_at)
  VALUES(target,p_tenant,'v2',source.collection_id,source.source_revision,
   'user',p_actor,p_input_digest,source.item_count,
   clock_timestamp()+interval '1 hour');
  INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,
   asset_version_id,artifact_id,sha256,payload)
  SELECT p_tenant,target,i.ordinal,i.item_id,i.asset_version_id,i.artifact_id,
   i.sha256,i.payload FROM photo.snapshot_items i
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=p_selection ORDER BY i.ordinal;
  GET DIAGNOSTICS copied=ROW_COUNT;
  IF copied<>source.item_count THEN
   RAISE EXCEPTION 'V22_ANALYSIS_SNAPSHOT_INCOMPLETE' USING ERRCODE='23514';
  END IF;
  UPDATE photo.snapshots SET expires_at=NULL WHERE tenant_id=p_tenant AND id=target;
 END IF;
 RETURN QUERY SELECT target,preflight || jsonb_build_object(
  'frozen_snapshot_id',target,'frozen_snapshot_digest',p_input_digest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)
 TO photo_scheduler;

-- The transient non-null expiry is a construction marker inside the freeze
-- transaction. A sealed V2 snapshot (expires_at NULL) cannot be reopened.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_tenant_purge_active(p_tenant uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tenants t JOIN photo.deletion_jobs j
  ON j.tenant_id=t.id AND j.resource_type='tenant' AND j.status='running'
  WHERE t.id=p_tenant AND t.status='deleting' AND j.cleanup_token IS NOT NULL
   AND j.cleanup_lease_until>clock_timestamp())
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_tenant_purge_active(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_tenant_purge_active(uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v22_frozen_snapshot() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.kind='v2' AND OLD.expires_at IS NULL THEN
  IF TG_OP='DELETE' THEN
   IF photo.v22_tenant_purge_active(OLD.tenant_id) THEN RETURN OLD; END IF;
   RAISE EXCEPTION 'V22_FROZEN_SNAPSHOT_IMMUTABLE' USING ERRCODE='23514';
  END IF;
  IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
   RAISE EXCEPTION 'V22_FROZEN_SNAPSHOT_IMMUTABLE' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_frozen_snapshot() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_frozen_snapshot() FROM PUBLIC;
CREATE TRIGGER v22_frozen_snapshot_guard BEFORE UPDATE OR DELETE ON photo.snapshots
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_frozen_snapshot();

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v22_frozen_items() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sealed boolean; target_tenant uuid; target_snapshot uuid;
BEGIN
 IF TG_OP='DELETE' THEN
  target_tenant:=OLD.tenant_id; target_snapshot:=OLD.snapshot_id;
 ELSE
  target_tenant:=NEW.tenant_id; target_snapshot:=NEW.snapshot_id;
 END IF;
 SELECT s.kind='v2' AND s.expires_at IS NULL INTO sealed FROM photo.snapshots s
  WHERE s.tenant_id=target_tenant AND s.id=target_snapshot FOR SHARE;
 IF sealed THEN
  IF TG_OP='DELETE' AND photo.v22_tenant_purge_active(target_tenant) THEN
   RETURN OLD;
  END IF;
  RAISE EXCEPTION 'V22_FROZEN_ITEMS_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_frozen_items() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_frozen_items() FROM PUBLIC;
CREATE TRIGGER v22_frozen_items_guard BEFORE INSERT OR UPDATE OR DELETE ON photo.snapshot_items
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_frozen_items();
