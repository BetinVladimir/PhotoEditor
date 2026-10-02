-- +goose Up
ALTER TABLE photo.capture_assessment_jobs ADD COLUMN capture_snapshot_id uuid;
ALTER TABLE photo.capture_assessment_jobs ADD CONSTRAINT capture_snapshot_fk
 FOREIGN KEY(tenant_id,capture_snapshot_id) REFERENCES photo.snapshots(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
-- +goose StatementBegin
CREATE FUNCTION photo.freeze_capture_snapshot(p_tenant uuid,p_job uuid,p_token uuid,p_snapshot uuid,p_jcs text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE; receipt photo.capture_receipts%ROWTYPE;
 actor uuid; body jsonb; packet_sha text; version photo.asset_versions%ROWTYPE;
BEGIN
 IF p_jcs IS NULL OR octet_length(p_jcs)>1048576 OR p_snapshot IS NULL THEN RETURN NULL; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token OR j.lease_until<=clock_timestamp() THEN RETURN NULL; END IF;
 body:=p_jcs::jsonb;packet_sha:=encode(sha256(convert_to(p_jcs,'UTF8')),'hex');
 IF body->>'schema_version' IS DISTINCT FROM '1' OR (body->>'snapshot_id')::uuid IS DISTINCT FROM p_snapshot
  OR (body->>'receipt_id')::uuid IS DISTINCT FROM j.receipt_id OR (body->>'session_id')::uuid IS DISTINCT FROM j.session_id
  OR (body->'binding'->>'asset_version_id')::uuid IS DISTINCT FROM j.asset_version_id THEN RETURN NULL; END IF;
 SELECT * INTO receipt FROM photo.capture_receipts WHERE tenant_id=p_tenant AND project_id=j.project_id AND id=j.receipt_id;
 SELECT created_by INTO actor FROM photo.capture_devices WHERE tenant_id=p_tenant AND project_id=j.project_id AND id=receipt.device_id;
 IF actor IS NULL OR NOT EXISTS(SELECT 1 FROM photo.memberships m JOIN photo.projects p ON p.tenant_id=m.tenant_id AND p.id=j.project_id
  WHERE m.tenant_id=p_tenant AND m.user_id=actor AND m.status='active' AND m.role IN('owner','admin','editor')
   AND p.archived_at IS NULL AND (p.visibility<>'restricted' OR m.role IN('owner','admin') OR EXISTS(
    SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id AND acl.user_id=actor AND acl.role='editor')))
 THEN RETURN NULL; END IF;
 SELECT * INTO version FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=j.asset_version_id;
 IF NOT FOUND OR version.master_artifact_id IS DISTINCT FROM (body->'binding'->'source'->>'artifact_id')::uuid
  OR version.content_hash IS DISTINCT FROM body->'binding'->'source'->>'sha256' THEN RETURN NULL; END IF;
 IF j.capture_snapshot_id IS NOT NULL THEN
  IF j.capture_snapshot_id<>p_snapshot OR NOT EXISTS(SELECT 1 FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=p_snapshot
   AND s.digest=packet_sha AND s.principal_id=actor AND s.kind='v2' AND s.expires_at IS NULL) THEN
   RAISE EXCEPTION 'CAPTURE_SNAPSHOT_CONFLICT' USING ERRCODE='23514'; END IF;
  RETURN actor;
 END IF;
 INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at)
 VALUES(p_snapshot,p_tenant,'v2','user',actor,packet_sha,1,clock_timestamp()+interval '1 hour');
 INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload)
 VALUES(p_tenant,p_snapshot,0,j.asset_version_id,version.master_artifact_id,version.content_hash,body->'binding');
 UPDATE photo.snapshots SET expires_at=NULL WHERE tenant_id=p_tenant AND id=p_snapshot;
 UPDATE photo.capture_assessment_jobs SET capture_snapshot_id=p_snapshot WHERE tenant_id=p_tenant AND id=p_job;
 RETURN actor;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.freeze_capture_snapshot(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.freeze_capture_snapshot(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.freeze_capture_snapshot(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
