-- +goose Up
-- Version-pinned original object completion after IO, repeating live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v29_perspective_line_detection_document(p_tenant uuid,p_run uuid,p_owner uuid,
 p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; ticket record; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token
 OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 SELECT * INTO ticket FROM photo.reserve_v29_perspective_line_detection_document(p_tenant,p_run,p_owner,p_sha,p_bytes,p_bucket,p_params);
 IF NOT FOUND OR ticket.artifact_id IS DISTINCT FROM p_artifact OR ticket.token IS DISTINCT FROM p_token THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version;END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
 WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'LINE_DETECTION_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v29_perspective_line_detection_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v29_perspective_line_detection_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.complete_v29_perspective_line_detection_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
