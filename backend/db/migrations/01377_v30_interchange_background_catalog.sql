-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_interchange_prepared_document(p_tenant uuid,p_workflow uuid,p_parsed text,p_entry uuid,p_binding jsonb,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;w photo.v2_workflows%ROWTYPE;certificate jsonb;receipt jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_artifact IS NULL OR p_entry IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 -- Match the lock order of reserve/complete before authority helpers acquire locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=w.run_id
 AND b.owner_id=p_artifact AND b.artifact_id=p_artifact AND b.purpose='v30_import_prepared';
 IF NOT FOUND OR d.state<>'ready' OR d.sha256 IS DISTINCT FROM p_sha
 OR d.v30_import_prepared->>'contract' IS DISTINCT FROM p_contract
 OR d.v30_import_prepared->>'entry' IS DISTINCT FROM p_entry::text
 OR d.v30_import_prepared->'binding' IS DISTINCT FROM p_binding THEN RETURN;END IF;
 receipt:=jsonb_build_object('entry_id',p_entry,'binding',p_binding,'sha256',d.sha256,'bytes',d.bytes,'media_type',d.v30_import_prepared->>'media_type');
 IF d.v30_import_prepared ? 'warp_occurrence' THEN receipt:=receipt||jsonb_build_object('warp_occurrence',d.v30_import_prepared->'warp_occurrence');END IF;
 IF d.v30_import_prepared ? 'background_occurrence' THEN receipt:=receipt||jsonb_build_object('background_occurrence',d.v30_import_prepared->'background_occurrence');END IF;
 certificate:=photo.v30_interchange_prepared_scope(p_tenant,p_workflow,p_parsed,p_artifact,p_contract,receipt::text);
 IF certificate IS NULL OR certificate IS DISTINCT FROM d.v30_import_prepared THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.output_role='v30_import_prepared' AND a.object_version NOT IN('','null')
 AND a.media_type=certificate->>'media_type'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||w.run_id||'/documents/'||p_artifact
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
END $$;
ALTER FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_prepared_document(uuid,uuid,text,uuid,jsonb,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
