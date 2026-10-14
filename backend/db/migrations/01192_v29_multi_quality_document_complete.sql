-- +goose Up
-- Object IO is performed by the existing immutable publisher outside this transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v29_multi_quality_document(p_tenant uuid,p_run uuid,p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text,p_root text,p_image text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; pinned uuid; variant text; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 IF p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.v29_multi_quality_params IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact
 OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 variant:='master';
 pinned:=photo.pin_v29_multi_quality_document(p_tenant,p_run,variant,p_root,p_image,d.v29_multi_quality_params,p_bucket);
 IF pinned IS DISTINCT FROM p_artifact THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.sha256=p_sha AND a.bytes=p_bytes
  AND a.bucket=p_bucket AND a.object_key=target AND a.object_version=p_version AND a.output_role='task_params'
  AND a.media_type='application/json' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
 WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DERIVATIVE_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v29_multi_quality_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v29_multi_quality_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v29_multi_quality_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
