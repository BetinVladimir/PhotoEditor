-- +goose Up
-- Accepted geometry version and its exact quality/Apply decision are required.
-- This helper never approves a result or creates an asset version.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_accepted_perspective_parent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_task uuid,p_result uuid,p_root uuid,p_root_sha text,p_image uuid,p_image_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE matches integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT count(*) INTO matches
 FROM photo.runs quality
 JOIN photo.backend_documents d ON d.tenant_id=quality.tenant_id AND d.run_id=quality.id
 AND d.purpose='task_params' AND d.state='ready' AND d.v29_perspective_apply_decision IS NOT NULL
 CROSS JOIN LATERAL photo.v29_perspective_quality_review(p_tenant,p_project,
 (d.v29_perspective_apply_decision->>'base_version_id')::uuid,p_task,p_actor,p_session,p_result,p_root_sha) q
 JOIN photo.asset_versions accepted ON accepted.tenant_id=p_tenant
 AND accepted.id=photo.v30_uuid5(q.quality_run_id,'V29.PerspectiveAppliedVersion')
 JOIN photo.assets asset ON asset.tenant_id=p_tenant AND asset.id=accepted.asset_id
 JOIN photo.asset_versions base ON base.tenant_id=p_tenant AND base.id=accepted.parent_asset_version_id AND base.asset_id=asset.id
 WHERE quality.tenant_id=p_tenant AND quality.id=q.quality_run_id AND quality.v29_perspective_quality_task_id=p_task
 AND d.owner_id=q.quality_task_id AND accepted.created_by_run_id=q.quality_run_id
 AND accepted.review_state='accepted' AND accepted.master_artifact_id=p_image AND accepted.content_hash=p_image_sha
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND base.id=(d.v29_perspective_apply_decision->>'base_version_id')::uuid
 AND d.v29_perspective_apply_decision->'decision'->'schema_version'='1'::jsonb
 AND d.v29_perspective_apply_decision->'decision'->'confirm_geometry_reset'='true'::jsonb
 AND d.v29_perspective_apply_decision->'decision'->>'result_id'=p_result::text
 AND d.v29_perspective_apply_decision->'decision'->'root'=jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha)
 AND d.v29_perspective_apply_decision->'decision'->>'quality_run_id'=q.quality_run_id::text
 AND d.v29_perspective_apply_decision->'decision'->>'quality_result_id'=q.quality_result_id::text
 AND d.v29_perspective_apply_decision->'decision'->'quality_report'=jsonb_build_object('artifact_id',q.report_id,'sha256',q.report_sha)
 AND q.report::jsonb->'target'=jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha)
 AND (q.report::jsonb->>'status'='pass' OR (q.report::jsonb->>'status'='review'
 AND d.v29_perspective_apply_decision->'decision'->'accept_quality_warnings'='true'::jsonb))
 AND EXISTS(SELECT 1 FROM photo.asset_version_sources src WHERE src.tenant_id=p_tenant AND src.version_id=accepted.id
 AND src.source_version_id=base.id AND src.role='perspective_source' AND src.ordinal=0);
 RETURN matches=1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_accepted_perspective_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_accepted_perspective_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text;old_gate text;
BEGIN
 definition:=pg_get_functiondef('photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 old_gate:='WHERE s.route=''candidate'' AND s.image_id=image.id AND s.image_sha=image.sha256)))';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'PERSPECTIVE_PARENT_REVIEW_GATE_BASELINE_MISMATCH';END IF;
 definition:=replace(definition,old_gate,
 'WHERE s.route=''candidate'' AND s.image_id=image.id AND s.image_sha=image.sha256)) OR (t.operation=''optical.perspective'' AND photo.v29_accepted_perspective_parent(p_tenant,p_project,p_actor,p_session,t.id,z.id,root.id,root.sha256,image.id,image.sha256)))');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
