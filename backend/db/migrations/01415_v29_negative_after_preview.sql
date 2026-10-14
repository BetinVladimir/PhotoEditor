-- +goose Up
-- Keep render batch readiness restricted; add negative only to derivatives.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_task_result_ready(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id::text=z.outputs->0->>'artifact_id'
  WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded' AND t.operation='optical.negative' AND t.operation_version='1'
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND jsonb_array_length(z.outputs)=1
   AND a.sha256=z.outputs->0->>'sha256' AND a.output_role='document' AND a.media_type='application/json'
   AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_negative_task_result_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_task_result_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 -- V27 wrapped this source in 00616; patch the retained V26 branch.
 definition:=pg_get_functiondef('photo.v26_render_derivative_source_before_exposure(uuid,uuid)'::regprocedure);
 needle:='photo.v26_render_task_result_ready(p_tenant,p_task)';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_DERIVATIVE_SOURCE_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,'(photo.v26_render_task_result_ready(p_tenant,p_task) OR photo.v29_negative_task_result_ready(p_tenant,p_task))');
 definition:=pg_get_functiondef('photo.enqueue_next_v26_render_derivative(text)'::regprocedure);
 needle:='t.operation=''render.compose''';
 IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'NEGATIVE_DERIVATIVE_POLL_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,'t.operation IN (''render.compose'',''optical.negative'')');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
