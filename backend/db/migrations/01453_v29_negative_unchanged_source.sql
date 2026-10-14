-- +goose Up
-- Resolve only a ready source already frozen in this original consumer's input
-- inventory. Changed or invalidated producer output is never an unchanged input.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_unchanged_source(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_task uuid,p_source uuid,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;changed jsonb;result jsonb;
BEGIN
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 IF intent IS NULL OR p_source IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.review_id=p_review AND i.decision_hash=p_hash AND i.route='base') THEN RETURN NULL;END IF;
 changed:=photo.read_v29_negative_affected_source_inputs(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 IF changed IS NULL OR EXISTS(SELECT 1 FROM jsonb_array_elements(changed) input WHERE input->>'artifact_id'=p_source::text) THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,
 'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) INTO result
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_source AND a.sha256=p_sha
 AND a.state='ready' AND a.output_role='working_image' AND a.media_type='application/json'
 AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 65536
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.v29_negative_invalidations invalid WHERE invalid.tenant_id=a.tenant_id AND invalid.task_id=a.created_by_task_id)
 AND (EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=p_project
 AND input.task_id=p_task AND input.artifact_id=a.id AND input.sha256=a.sha256 AND input.contract='WorkingImage')
 OR EXISTS(SELECT 1 FROM photo.tasks consumer WHERE consumer.tenant_id=p_tenant AND consumer.id=p_task
 AND consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))));
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_unchanged_source(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_unchanged_source(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_unchanged_source(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
