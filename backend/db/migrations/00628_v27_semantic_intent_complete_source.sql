-- +goose Up
-- Recheck and lock accepted source inventory after object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v27_semantic_intent_upload(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_object_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; target text; project uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_object_version IS NULL OR p_object_version IN('','null') OR length(p_object_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT asset.project_id INTO project FROM photo.asset_versions v
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.tenants tenant ON tenant.id=v.tenant_id AND tenant.status='active'
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.review_state='accepted'
 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 FOR UPDATE OF asset FOR SHARE OF v,source,original;
 IF project IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.project_id<>project OR d.upload_contract<>'Document' OR d.asset_version_id<>p_version OR d.artifact_id<>p_artifact OR d.token<>p_token
  OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_object_version;END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/v2-drafts/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_object_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'document');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DRAFT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v2_draft_uploads SET state='ready',object_version=p_object_version,completed_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v27_semantic_intent_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v27_semantic_intent_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v27_semantic_intent_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
