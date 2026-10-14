-- +goose Up
-- Resolve changed source occurrences from the persisted original consumer input
-- inventory. No source ref/producer mapping is accepted from the client.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_affected_source_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_review uuid,p_hash text,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;result jsonb;
BEGIN
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task);
 IF intent IS NULL THEN RETURN NULL;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',source.id,'sha256',source.sha256,
 'producer_task_id',producer.id,'producer_run_id',producer.run_id) ORDER BY source.id),'[]'::jsonb) INTO result
 FROM photo.artifacts source JOIN photo.tasks producer ON producer.tenant_id=source.tenant_id AND producer.id=source.created_by_task_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=producer.tenant_id AND workflow.run_id=producer.run_id AND workflow.project_id=p_project
 WHERE source.tenant_id=p_tenant AND source.output_role='working_image' AND source.media_type='application/json'
 AND EXISTS(SELECT 1 FROM photo.v29_negative_invalidations consumer WHERE consumer.tenant_id=p_tenant
 AND consumer.task_id=p_task AND consumer.review_id=p_review AND consumer.decision_hash=p_hash AND consumer.route='base'
 AND (consumer.origin_task_id=producer.id OR EXISTS(SELECT 1 FROM photo.v29_negative_invalidations parent
 WHERE parent.tenant_id=consumer.tenant_id AND parent.review_id=consumer.review_id AND parent.decision_hash=consumer.decision_hash
 AND parent.origin_task_id=consumer.origin_task_id AND parent.route='base' AND parent.task_id=producer.id)))
 AND (EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=p_project
 AND input.task_id=p_task AND input.artifact_id=source.id AND input.sha256=source.sha256 AND input.contract='WorkingImage')
 OR EXISTS(SELECT 1 FROM photo.tasks consumer WHERE consumer.tenant_id=p_tenant AND consumer.id=p_task
 AND consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',source.id::text,'sha256',source.sha256))));
 IF jsonb_array_length(result)>30 OR pg_column_size(result)>32768 THEN RETURN NULL;END IF;
 RETURN result;
END $$;
ALTER FUNCTION photo.read_v29_negative_affected_source_inputs(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_affected_source_inputs(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_affected_source_inputs(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
