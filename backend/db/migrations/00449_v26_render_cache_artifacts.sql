-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_cache_artifacts(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_key text,p_body text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,media_type text,output_role text,bucket text,object_key text,object_version text,ordinal integer)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidate AS MATERIALIZED (
 SELECT * FROM photo.lookup_v26_render_cache(p_tenant,p_task,p_attempt,p_fence,p_key,p_body)
 ), entries AS (
 SELECT e.value,e.ordinality FROM candidate c,jsonb_array_elements(c.inventory) WITH ORDINALITY e
 )
 SELECT a.id,a.sha256,a.bytes,a.media_type,a.output_role,a.bucket,a.object_key,a.object_version,(e.ordinality-1)::integer
 FROM entries e JOIN photo.artifacts a ON a.tenant_id=p_tenant
 AND a.id=(e.value->>'artifact_id')::uuid AND a.sha256=e.value->>'sha256'
 ORDER BY e.ordinality
$$;
ALTER FUNCTION photo.v26_render_cache_artifacts(uuid,uuid,integer,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_cache_artifacts(uuid,uuid,integer,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_cache_artifacts(uuid,uuid,integer,bigint,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
