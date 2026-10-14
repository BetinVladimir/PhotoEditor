-- +goose Up
-- Resolve original decline decision for an exact saved detector descriptor.
-- Discovery only; source IO and full mapped physical locks remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_replacement_detector_decision(p_tenant uuid,p_run uuid,p_descriptor text,p_snapshot uuid)
RETURNS TABLE(origin_task_id uuid,origin_run_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 3145728 THEN RETURN;END IF;
 d:=p_descriptor::jsonb;
 IF photo.v29_negative_invalidation_authorized(p_tenant,(d->>'project_id')::uuid,(d->>'actor_id')::uuid,(d->>'auth_session_id')::uuid) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT review.task_id,review.run_id FROM photo.runs r
 JOIN photo.reviews review ON review.tenant_id=r.tenant_id AND review.id=(d->>'review_id')::uuid
 CROSS JOIN LATERAL photo.v29_negative_review_selection(p_tenant,review.task_id) selection
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.input_snapshot_id=p_snapshot
 AND r.config_snapshot->>'v29_perspective_replacement_detection_jcs'=p_descriptor
 AND d->>'tenant_id'=p_tenant::text AND d->>'run_id'=p_run::text
 AND selection.review_id=review.id AND selection.decision_hash=d->>'decision_hash' AND selection.route='base';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_decision(uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_decision(uuid,uuid,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_decision(uuid,uuid,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
