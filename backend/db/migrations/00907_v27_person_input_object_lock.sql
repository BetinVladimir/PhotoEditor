-- +goose Up
-- Exact immutable person root/pixel object versions after original producer locks.
-- No admission, table, DML grant, profile rewrite or recovery backfill.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_person_input_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text,p_expected jsonb,p_objects jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF p_root=p_child OR jsonb_typeof(p_objects) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_objects)<>2 OR octet_length(p_objects::text)>16384
 OR NOT photo.lock_semantic_proposal_person_producer(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha,p_expected) THEN RETURN false; END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY CASE a.id WHEN p_root THEN 0 ELSE 1 END)
 INTO actual FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.state='ready' AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.object_version NOT IN ('','null')
 AND ((a.id=p_root AND a.sha256=p_root_sha AND a.media_type='application/json')
 OR (a.id=p_child AND a.sha256=p_child_sha AND a.media_type IN ('application/octet-stream','application/x-npy')));
 RETURN coalesce(jsonb_array_length(actual)=2 AND actual=p_objects,false);
END $$;
ALTER FUNCTION photo.lock_semantic_native_person_input_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_person_input_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_person_input_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
