-- +goose Up
-- Current leased negative input metadata; no output discovery or object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_worker_negative_input_evidence(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text,
 p_artifact uuid,p_artifact_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE answer jsonb;
BEGIN
 IF photo.v29_worker_negative_qualification_evidence(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
 p_params,p_sha,p_body,p_digest) IS NULL THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,
 'sha256',a.sha256,'bytes',a.bytes,'media_type',a.media_type,'role',a.output_role,'source','input') INTO answer
 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id
 AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=p_artifact AND i.sha256=p_artifact_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF photo.v29_negative_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,
 p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN answer;
END $$;
ALTER FUNCTION photo.v29_worker_negative_input_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_worker_negative_input_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_worker_negative_input_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
