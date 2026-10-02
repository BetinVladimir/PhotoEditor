-- Verified searchable projection of immutable scene_embedding artifacts.
-- +goose Up
CREATE TABLE photo.asset_embeddings(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL,asset_version_id uuid NOT NULL,artifact_id uuid NOT NULL,
 model_lock_id text,values float8[],state text NOT NULL DEFAULT 'materializing',token uuid NOT NULL DEFAULT gen_random_uuid(),lease_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '60 seconds',error_code text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,asset_version_id),UNIQUE(tenant_id,artifact_id),FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id),FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id),FOREIGN KEY(tenant_id,artifact_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK(state IN('materializing','ready','failed')),CHECK(model_lock_id IS NULL OR model_lock_id~'^[0-9a-f]{64}$'),CHECK(values IS NULL OR cardinality(values)=384),CHECK((state='ready')=(values IS NOT NULL AND model_lock_id IS NOT NULL))
);
ALTER TABLE photo.asset_embeddings ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.asset_embeddings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.asset_embeddings TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.asset_embeddings TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT ON photo.asset_embeddings TO photo_api;GRANT SELECT,INSERT,UPDATE ON photo.asset_embeddings TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_asset_embedding() RETURNS TABLE(id uuid,token uuid,tenant_id uuid,asset_version_id uuid,source_artifact_id uuid,source_sha256 text,artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.asset_embeddings%ROWTYPE;
BEGIN
 INSERT INTO photo.asset_embeddings(tenant_id,asset_version_id,artifact_id)
 SELECT t.tenant_id,v.id,a.id FROM photo.tasks t JOIN photo.task_results r ON r.tenant_id=t.tenant_id AND r.id=t.canonical_result_id JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role='scene_embedding' AND a.state='ready' JOIN photo.asset_versions v ON v.tenant_id=t.tenant_id AND (t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',v.master_artifact_id::text,'sha256',v.content_hash))) LEFT JOIN photo.asset_embeddings old ON old.tenant_id=v.tenant_id AND old.asset_version_id=v.id WHERE t.operation='features.scene' AND t.state='succeeded' AND r.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)) AND old.id IS NULL ORDER BY t.created_at LIMIT 1 ON CONFLICT DO NOTHING RETURNING * INTO e;
 IF e.id IS NULL THEN SELECT * INTO e FROM photo.asset_embeddings x WHERE x.state='materializing' AND x.lease_until<=clock_timestamp() ORDER BY x.created_at LIMIT 1 FOR UPDATE SKIP LOCKED;IF e.id IS NOT NULL THEN UPDATE photo.asset_embeddings SET token=gen_random_uuid(),lease_until=clock_timestamp()+interval '60 seconds' WHERE asset_embeddings.id=e.id RETURNING * INTO e;END IF;END IF;
 IF e.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT e.id,e.token,e.tenant_id,e.asset_version_id,v.master_artifact_id,v.content_hash,e.artifact_id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type FROM photo.artifacts a JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id AND v.id=e.asset_version_id WHERE a.tenant_id=e.tenant_id AND a.id=e.artifact_id AND a.state='ready' AND a.object_version IS NOT NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_asset_embedding() OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.claim_asset_embedding() FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.claim_asset_embedding() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_asset_embedding(p_id uuid,p_token uuid,p_lock text,p_values float8[],p_error text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.asset_embeddings%ROWTYPE;n float8;
BEGIN
 SELECT * INTO e FROM photo.asset_embeddings WHERE id=p_id FOR UPDATE;IF NOT FOUND OR e.state<>'materializing' OR e.token<>p_token OR e.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF p_error IS NOT NULL THEN UPDATE photo.asset_embeddings SET state='failed',error_code=p_error,lease_until=clock_timestamp() WHERE id=p_id;RETURN true;END IF;
 IF p_lock !~ '^[0-9a-f]{64}$' OR cardinality(p_values)<>384 OR EXISTS(SELECT FROM unnest(p_values) x WHERE x::text IN('NaN','Infinity','-Infinity')) THEN RETURN false;END IF;
 SELECT sqrt(sum(x*x)) INTO n FROM unnest(p_values) x;IF n NOT BETWEEN .999 AND 1.001 THEN RETURN false;END IF;
 UPDATE photo.asset_embeddings SET state='ready',model_lock_id=p_lock,values=p_values,error_code=NULL,lease_until=clock_timestamp() WHERE id=p_id;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_asset_embedding(uuid,uuid,text,float8[],text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.finish_asset_embedding(uuid,uuid,text,float8[],text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.finish_asset_embedding(uuid,uuid,text,float8[],text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
