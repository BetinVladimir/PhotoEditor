-- +goose Up
-- Read preliminary persisted bytes only. No creation, retry or admission effect.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_manual_perspective_replacement_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_original uuid,p_snapshot uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;namespace uuid;new_run uuid;new_task uuid;workflow uuid;marker jsonb;result text;
BEGIN
 IF p_snapshot IS NULL OR p_snapshot='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN NULL;END IF;
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_original);
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM 'optical.perspective' THEN RETURN NULL;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||p_review::text||':'||p_hash);
 new_task:=photo.v30_uuid5(namespace,'task:'||p_original::text);
 new_run:=photo.v30_uuid5(namespace,'run:'||(intent->>'original_run_id')||':'||p_original::text);
 workflow:=photo.v30_uuid5(new_run,'workflow');
 marker:=jsonb_build_object('version','v29_negative_replacement_v2','project_id',p_project::text,
 'review_id',p_review::text,'decision_hash',p_hash,'original_run_id',intent->>'original_run_id','original_task_id',p_original::text);
 SELECT r.config_snapshot->>'v29_negative_replacement_draft_jcs' INTO result
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tasks original ON original.tenant_id=r.tenant_id AND original.id=p_original
 JOIN photo.runs original_run ON original_run.tenant_id=original.tenant_id AND original_run.id=original.run_id
 JOIN photo.v2_workflows original_workflow ON original_workflow.tenant_id=original.tenant_id AND original_workflow.run_id=original.run_id
 WHERE r.tenant_id=p_tenant AND r.id=new_run AND r.created_by=p_actor AND r.kind='v2'
 AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.input_snapshot_id=p_snapshot
 AND w.id=workflow AND w.project_id=p_project AND w.input_snapshot_id=p_snapshot
 AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND w.policy_sha=(r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb->'draft'->'execution_profile'->'perspective_qualification'->>'policy_sha'
 AND original.run_id=(intent->>'original_run_id')::uuid AND original.operation='optical.perspective' AND original.operation_version='1'
 AND original.state IN('succeeded','failed','cancelled','skipped') AND original_run.input_snapshot_id=p_snapshot
 AND original_workflow.project_id=p_project AND original_workflow.input_snapshot_id=p_snapshot
 AND r.config_snapshot->'v29_negative_replacement'=marker
 AND jsonb_typeof(r.config_snapshot->'v29_negative_replacement_draft_jcs')='string'
 AND (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(r.config_snapshot) k)=ARRAY['project_id','v29_negative_replacement','v29_negative_replacement_draft_jcs']::text[]
 AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_hash=encode(sha256(convert_to(r.config_snapshot::text,'UTF8')),'hex');
 -- The stored draft is a JSON string, not a nested jsonb document.
 IF result IS NULL OR octet_length(result) NOT BETWEEN 2 AND 3145728 THEN RETURN NULL;END IF;
 IF (result::jsonb)->'draft'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR (result::jsonb)->'draft'->>'actor_id' IS DISTINCT FROM p_actor::text
 OR (result::jsonb)->'identity'->>'task_id' IS DISTINCT FROM new_task::text
 OR (result::jsonb)->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR (result::jsonb)->'identity'->>'operation' IS DISTINCT FROM 'optical.perspective'
 OR (result::jsonb)->'draft'->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR ((result::jsonb)->'draft'->>'params_jcs')::jsonb->>'mode' IS DISTINCT FROM 'manual'
 OR (result::jsonb)->'draft' ? 'lines_jcs' THEN RETURN NULL;END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_manual_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_manual_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_manual_perspective_replacement_draft(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
