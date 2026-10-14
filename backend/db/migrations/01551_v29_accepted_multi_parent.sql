-- +goose Up
-- A reviewed multi result becomes a parent only after explicit quality/Apply.
-- Physical WorkingImage/array reads and replacement membership remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_accepted_multi_parent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_task uuid,p_result uuid,p_root uuid,p_root_sha text,p_image uuid,p_image_sha text,p_operation text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE matches integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_operation NOT IN('optical.panorama','optical.focus_stack') OR p_operation IS NULL
 OR photo.v29_negative_invalidation_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT count(*) INTO matches
 FROM photo.runs quality
 JOIN photo.backend_documents d ON d.tenant_id=quality.tenant_id AND d.run_id=quality.id
 AND d.purpose='task_params' AND d.state='ready' AND d.v29_multi_apply_decision IS NOT NULL
 CROSS JOIN LATERAL photo.v29_multi_quality_review(p_tenant,p_project,p_operation,p_task,p_actor,p_session,p_result,p_root_sha) q
 JOIN photo.asset_versions accepted ON accepted.tenant_id=p_tenant
 AND accepted.id=photo.v30_uuid5(q.quality_run_id,'V29.MultiAppliedVersion')
 JOIN photo.assets asset ON asset.tenant_id=p_tenant AND asset.id=accepted.asset_id
 CROSS JOIN LATERAL (SELECT (quality.config_snapshot->>'multi_params_jcs')::jsonb AS body) params
 WHERE quality.tenant_id=p_tenant AND quality.id=q.quality_run_id AND quality.v29_multi_quality_task_id=p_task
 AND quality.config_snapshot->>'operation'=p_operation
 AND d.owner_id=q.quality_task_id AND d.v29_multi_quality_receipt IS NOT NULL
 AND accepted.created_by_run_id=q.quality_run_id AND accepted.review_state='accepted'
 AND accepted.master_artifact_id=p_image AND accepted.content_hash=p_image_sha
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND jsonb_array_length(params.body->'frames') BETWEEN 2 AND 30
 AND accepted.parent_asset_version_id=(params.body->'frames'->((params.body->>'reference_ordinal')::integer)->>'asset_version_id')::uuid
 AND d.v29_multi_apply_decision->>'operation'=p_operation
 AND d.v29_multi_apply_decision->'sources'=params.body->'frames'
 AND d.v29_multi_apply_decision->'decision'->'schema_version'='1'::jsonb
 AND d.v29_multi_apply_decision->'decision'->'confirm_geometry_reset'='true'::jsonb
 AND d.v29_multi_apply_decision->'decision'->>'result_id'=p_result::text
 AND d.v29_multi_apply_decision->'decision'->'root'=jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha)
 AND d.v29_multi_apply_decision->'decision'->>'quality_run_id'=q.quality_run_id::text
 AND d.v29_multi_apply_decision->'decision'->>'quality_result_id'=q.quality_result_id::text
 AND d.v29_multi_apply_decision->'decision'->'quality_report'=jsonb_build_object('artifact_id',q.report_id,'sha256',q.report_sha)
 AND q.report::jsonb->'target'=jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha)
 AND (q.report::jsonb->>'status'='pass' OR (q.report::jsonb->>'status'='review'
 AND d.v29_multi_apply_decision->'decision'->'accept_quality_warnings'='true'::jsonb))
 AND (SELECT count(*) FROM photo.asset_version_sources x WHERE x.tenant_id=p_tenant AND x.version_id=accepted.id)=jsonb_array_length(params.body->'frames')
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(params.body->'frames') WITH ORDINALITY f(frame,ord)
 LEFT JOIN photo.asset_version_sources x ON x.tenant_id=p_tenant AND x.version_id=accepted.id
 AND x.source_version_id=(f.frame->>'asset_version_id')::uuid AND x.role=p_operation AND x.ordinal=f.ord-1
 LEFT JOIN photo.asset_versions source ON source.tenant_id=p_tenant AND source.id=x.source_version_id AND source.review_state='accepted'
 LEFT JOIN photo.assets source_asset ON source_asset.tenant_id=p_tenant AND source_asset.id=source.asset_id
 AND source_asset.project_id=p_project AND source_asset.deleted_at IS NULL AND source_asset.upload_state='verified'
 WHERE x.version_id IS NULL OR source.id IS NULL OR source_asset.id IS NULL)
 -- Retained Apply must have preserved both document anchors in the source workflow.
 AND NOT EXISTS(SELECT 1 FROM (VALUES(p_root,p_root_sha),(p_image,p_image_sha)) required(id,sha)
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links link WHERE link.tenant_id=p_tenant AND link.project_id=p_project
 AND link.workflow_id=(quality.config_snapshot->>'source_workflow_id')::uuid
 AND link.purpose='multi.applied:'||accepted.id::text AND link.artifact_id=required.id AND link.sha256=required.sha));
 RETURN matches=1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_accepted_multi_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_accepted_multi_parent(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE definition text;old_gate text;
BEGIN
 definition:=pg_get_functiondef('photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)'::regprocedure);
 old_gate:='OR (t.operation=''optical.perspective'' AND photo.v29_accepted_perspective_parent(p_tenant,p_project,p_actor,p_session,t.id,z.id,root.id,root.sha256,image.id,image.sha256)))';
 IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'MULTI_PARENT_REVIEW_GATE_BASELINE_MISMATCH';END IF;
 EXECUTE replace(definition,old_gate,
 'OR (t.operation=''optical.perspective'' AND photo.v29_accepted_perspective_parent(p_tenant,p_project,p_actor,p_session,t.id,z.id,root.id,root.sha256,image.id,image.sha256)) OR (t.operation IN(''optical.panorama'',''optical.focus_stack'') AND photo.v29_accepted_multi_parent(p_tenant,p_project,p_actor,p_session,t.id,z.id,root.id,root.sha256,image.id,image.sha256,t.operation)))');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
