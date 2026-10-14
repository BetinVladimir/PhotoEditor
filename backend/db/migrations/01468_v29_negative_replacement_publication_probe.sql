-- +goose Up
-- Explicit absence is distinct from stale authority or conflicting persistence.
-- No save/stage/receipt construction is performed by this probe.
-- +goose StatementBegin
CREATE FUNCTION photo.probe_v29_negative_replacement_publication(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE draft jsonb;identity jsonb;owner jsonb;intent jsonb;namespace uuid;new_run uuid;new_task uuid;marker jsonb;r photo.runs%ROWTYPE;
BEGIN
 IF p_expected IS NULL OR octet_length(p_expected) NOT BETWEEN 2 AND 3145728 THEN RETURN NULL;END IF;
 draft:=p_expected::jsonb;identity:=draft->'identity';owner:=draft->'owner';
 IF draft->'schema_version' IS DISTINCT FROM '2'::jsonb OR identity->'schema_version' IS DISTINCT FROM '2'::jsonb
 OR identity->>'operation' IS DISTINCT FROM 'optical.negative'
 OR owner->>'tenant_id' IS DISTINCT FROM p_tenant::text OR owner->>'project_id' IS DISTINCT FROM p_project::text
 OR owner->>'actor_id' IS DISTINCT FROM p_actor::text OR owner->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR identity->>'tenant_id' IS DISTINCT FROM p_tenant::text OR identity->>'project_id' IS DISTINCT FROM p_project::text THEN RETURN NULL;END IF;
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,
 identity->>'decision_hash',(identity->>'original_task_id')::uuid);
 IF intent IS NULL OR intent->>'operation' IS DISTINCT FROM 'optical.negative'
 OR intent->>'original_run_id' IS DISTINCT FROM identity->>'original_run_id'
 OR photo.lock_v29_negative_replacement_snapshot(p_tenant,p_project,p_actor,p_session,(identity->>'review_id')::uuid,
 identity->>'decision_hash',(identity->>'original_task_id')::uuid,(owner->>'snapshot_id')::uuid) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 namespace:=photo.v30_uuid5(p_tenant,'V29.NegativeReplacement/v2:'||p_project::text||':'||(identity->>'review_id')||':'||(identity->>'decision_hash'));
 new_task:=photo.v30_uuid5(namespace,'task:'||(identity->>'original_task_id'));
 new_run:=photo.v30_uuid5(namespace,'run:'||(identity->>'original_run_id')||':'||(identity->>'original_task_id'));
 IF identity->>'task_id' IS DISTINCT FROM new_task::text OR owner->>'task_id' IS DISTINCT FROM new_task::text
 OR identity->>'run_id' IS DISTINCT FROM new_run::text OR owner->>'run_id' IS DISTINCT FROM new_run::text
 OR identity->>'workflow_id' IS DISTINCT FROM photo.v30_uuid5(new_run,'workflow')::text
 OR owner->>'workflow_id' IS DISTINCT FROM identity->>'workflow_id'
 OR identity->>'params_document_id' IS DISTINCT FROM photo.v30_uuid5(new_task,'params')::text THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=new_run FOR SHARE;
 IF NOT FOUND THEN RETURN 'absent';END IF;
 marker:=jsonb_build_object('version','v29_negative_replacement_v2','project_id',p_project::text,
 'review_id',identity->>'review_id','decision_hash',identity->>'decision_hash',
 'original_run_id',identity->>'original_run_id','original_task_id',identity->>'original_task_id');
 IF r.tenant_id IS DISTINCT FROM p_tenant OR r.created_by IS DISTINCT FROM p_actor OR r.kind IS DISTINCT FROM 'v2'
 OR r.input_snapshot_id IS DISTINCT FROM (owner->>'snapshot_id')::uuid OR r.cancel_requested
 OR r.config_snapshot IS DISTINCT FROM jsonb_build_object('project_id',p_project,'v29_negative_replacement',marker,'v29_negative_replacement_draft_jcs',p_expected)
 OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(r.config_snapshot::text,'UTF8')),'hex')
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=new_run
 AND w.id=photo.v30_uuid5(new_run,'workflow') AND w.project_id=p_project AND w.input_snapshot_id=r.input_snapshot_id
 AND w.workflow_type='preview' AND w.renderer_version='v2.2') THEN RETURN NULL;END IF;
 IF r.graph_sealed AND r.v29_negative_replacement_queued_receipt IS NOT NULL THEN RETURN 'admitted';END IF;
 IF NOT r.graph_sealed AND r.status='draft' AND r.v29_negative_replacement_queued_receipt IS NULL THEN RETURN 'draft';END IF;
 RETURN NULL;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.probe_v29_negative_replacement_publication(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.probe_v29_negative_replacement_publication(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.probe_v29_negative_replacement_publication(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
