-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.attach_v30_interchange_snapshot(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_artifact uuid,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;rid uuid;r photo.runs%ROWTYPE;s photo.snapshots%ROWTYPE;d photo.backend_documents%ROWTYPE;
BEGIN
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='Interchange.Parse.Pending' AND key=p_key FOR SHARE;
 scope:=photo.v30_interchange_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,p_artifact,'v30_interchange_snapshot',p_body);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=rid FOR UPDATE;
 IF r.id IS NULL OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=rid AND owner_id=rid AND purpose='v30_interchange_snapshot' FOR SHARE;
 IF d.id IS NULL OR d.state IS DISTINCT FROM 'ready' OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.sha256 IS DISTINCT FROM scope->>'sha' OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint
 OR d.object_version IS NULL OR d.object_version IN('','null') THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact AND state='ready'
 AND sha256=d.sha256 AND bytes=d.bytes AND bucket=d.bucket AND object_version=d.object_version
 AND object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||p_artifact
 AND media_type='application/json' AND output_role='v30_interchange_snapshot' AND verified_at IS NOT NULL
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id FOR UPDATE;
 IF s.id IS NULL OR s.kind IS DISTINCT FROM 'v2' OR s.principal_type IS DISTINCT FROM 'user' OR s.principal_id IS DISTINCT FROM p_actor
 OR s.digest IS DISTINCT FROM r.config_snapshot->>'snapshot_digest' OR s.item_count IS DISTINCT FROM 1
 OR s.id::text IS DISTINCT FROM r.config_snapshot->>'snapshot_id' THEN RETURN false;END IF;
 IF s.expires_at IS NULL THEN RETURN s.artifact_id IS NOT DISTINCT FROM p_artifact;END IF;
 IF s.expires_at<=clock_timestamp() OR s.artifact_id IS NOT NULL THEN RETURN false;END IF;
 UPDATE photo.snapshots SET artifact_id=p_artifact,expires_at=NULL WHERE tenant_id=p_tenant AND id=s.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.attach_v30_interchange_snapshot(uuid,uuid,uuid,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.attach_v30_interchange_snapshot(uuid,uuid,uuid,text,text,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.attach_v30_interchange_snapshot(uuid,uuid,uuid,text,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
