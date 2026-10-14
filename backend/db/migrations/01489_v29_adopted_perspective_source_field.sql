-- +goose Up
-- A reused dense field must occur at its exact path in the SHA-bound actual source.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_adopted_perspective_source_field(p_tenant uuid,p_project uuid,p_version uuid,
 p_task uuid,p_actor uuid,p_session uuid,p_root uuid,p_root_sha text,p_source uuid,p_source_sha text,
 p_document text,p_field uuid,p_field_sha text,p_path text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b record; image jsonb; step record;
BEGIN
 IF p_document IS NULL OR octet_length(p_document) NOT BETWEEN 2 AND 65536
 OR encode(sha256(convert_to(p_document,'UTF8')),'hex') IS DISTINCT FROM p_source_sha THEN RETURN;END IF;
 SELECT * INTO b FROM photo.v29_adopted_perspective_binding(p_tenant,p_project,p_version,p_task,p_actor,p_session);
 IF NOT FOUND OR b.artifact_id IS DISTINCT FROM p_root OR b.sha256 IS DISTINCT FROM p_root_sha
 OR b.source_artifact_id IS DISTINCT FROM p_source OR b.source_sha256 IS DISTINCT FROM p_source_sha THEN RETURN;END IF;
 image:=p_document::jsonb;
 FOR step IN SELECT value,ordinality FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY LOOP
  IF step.value->>'kind'='dense_warp'
   AND step.value->'inverse_field'=jsonb_build_object('artifact_id',p_field,'sha256',p_field_sha)
   AND p_path='Document.transform_chain.steps['||(step.ordinality-1)::text||'].inverse_field' THEN
   RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_field AND a.sha256=p_field_sha AND a.state='ready'
    AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 1 AND 2400065536
    AND a.media_type='application/x-npy' AND a.output_role='warp_field'
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project
     AND i.task_id=p_task AND i.artifact_id=p_field AND i.sha256=p_field_sha AND i.contract='NpyArray');
   RETURN;
  END IF;
 END LOOP;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v29_adopted_perspective_source_field(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_adopted_perspective_source_field(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_adopted_perspective_source_field(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
