-- +goose Up
-- Keep reviewable previews available before decision, but never present a
-- declined render candidate as a selected run output after reject/skip.
-- The previous receipt/physical-object verifier remains authoritative.
-- +goose StatementBegin
ALTER FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)
 RENAME TO v26_render_derivative_outputs_before_review_gate;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_outputs_before_review_gate(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker;
CREATE FUNCTION photo.v26_render_derivative_outputs(
 p_user uuid,p_session uuid,p_tenant uuid,p_source_run uuid)
RETURNS TABLE(artifact_id uuid,output_role text,sha256 text,bytes bigint,
 media_type text,bucket text,object_key text,object_version text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT prior.artifact_id,prior.output_role,prior.sha256,prior.bytes,
  prior.media_type,prior.bucket,prior.object_key,prior.object_version
 FROM photo.v26_render_derivative_outputs_before_review_gate(
  p_user,p_session,p_tenant,p_source_run) prior
 JOIN photo.artifacts output ON output.tenant_id=p_tenant
  AND output.id=prior.artifact_id AND output.sha256=prior.sha256
 JOIN photo.tasks derivative ON derivative.tenant_id=output.tenant_id
  AND derivative.id=output.created_by_task_id
 JOIN photo.runs preview ON preview.tenant_id=derivative.tenant_id
  AND preview.id=derivative.run_id AND preview.kind='preview'
 JOIN photo.tasks origin ON origin.tenant_id=preview.tenant_id
  AND origin.id=preview.v26_derivative_task_id AND origin.run_id=p_source_run
 WHERE NOT EXISTS(SELECT 1 FROM photo.reviews v
  WHERE v.tenant_id=origin.tenant_id AND v.task_id=origin.id
   AND v.kind='render.compose' AND v.state='decided'
   AND (v.decision='accept' AND v.decision_route='candidate'
    AND photo.v26_render_review_completion_ready(v.tenant_id,v.task_id)) IS NOT TRUE)
 ORDER BY derivative.node_key,prior.output_role,prior.artifact_id;
$$;
ALTER FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)
 TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
