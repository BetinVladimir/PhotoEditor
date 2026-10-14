-- +goose Up
-- Durable explicit decision only. Version creation is a separate atomic step.
ALTER TABLE photo.backend_documents ADD COLUMN v29_perspective_apply_decision jsonb;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_v29_perspective_apply_decision CHECK(
 v29_perspective_apply_decision IS NULL OR (purpose='task_params' AND v29_perspective_quality_receipt IS NOT NULL
 AND jsonb_typeof(v29_perspective_apply_decision)='object' AND octet_length(v29_perspective_apply_decision::text)<=65536));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_perspective_apply_decision() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.v29_perspective_apply_decision IS DISTINCT FROM OLD.v29_perspective_apply_decision
 AND (current_user<>'photo_fn_owner' OR OLD.v29_perspective_apply_decision IS NOT NULL) THEN
 RAISE EXCEPTION 'Perspective Apply decision is immutable' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_perspective_apply_decision() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_perspective_apply_decision() FROM PUBLIC;
-- +goose StatementEnd
CREATE TRIGGER backend_documents_v29_perspective_apply_guard BEFORE UPDATE OF v29_perspective_apply_decision ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_perspective_apply_decision();
-- +goose StatementBegin
CREATE FUNCTION photo.save_v29_perspective_apply_decision(p_tenant uuid,p_project uuid,p_version uuid,p_task uuid,
 p_actor uuid,p_session uuid,p_decision jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE q record;d photo.backend_documents%ROWTYPE;r photo.runs%ROWTYPE;stored jsonb;status text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_decision) IS DISTINCT FROM 'object' OR octet_length(p_decision::text)>65536
 OR NOT p_decision ?& ARRAY['schema_version','result_id','root','quality_run_id','quality_result_id','quality_report','confirm_geometry_reset','accept_quality_warnings']
 OR (p_decision-'schema_version'-'result_id'-'root'-'quality_run_id'-'quality_result_id'-'quality_report'-'confirm_geometry_reset'-'accept_quality_warnings')<>'{}'::jsonb
 OR p_decision->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_decision->'confirm_geometry_reset' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(p_decision->'accept_quality_warnings') IS DISTINCT FROM 'boolean' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(p_decision->>'quality_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.v29_perspective_quality_task_id IS DISTINCT FROM p_task THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'source_run_id')::uuid FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND (id=p_task OR run_id=r.id) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.asset_versions WHERE tenant_id=p_tenant AND id=p_version FOR SHARE;
 PERFORM a.id FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.run_id=r.id
 WHERE a.tenant_id=p_tenant AND (a.id=t.params_artifact_id OR a.created_by_task_id=t.id) FOR SHARE OF a;
 SELECT * INTO q FROM photo.v29_perspective_quality_review(p_tenant,p_project,p_version,p_task,p_actor,p_session,
 (p_decision->>'result_id')::uuid,p_decision->'root'->>'sha256');
 IF NOT FOUND OR q.quality_run_id IS DISTINCT FROM r.id
 OR q.quality_result_id::text IS DISTINCT FROM p_decision->>'quality_result_id'
 OR jsonb_build_object('artifact_id',q.report_id,'sha256',q.report_sha) IS DISTINCT FROM p_decision->'quality_report'
 OR p_decision->'root' IS DISTINCT FROM r.config_snapshot->'root' THEN RETURN false;END IF;
 status:=(q.report::jsonb)->>'status';
 IF status IS NULL OR status NOT IN('pass','review') OR (status='review' AND p_decision->'accept_quality_warnings'<>'true'::jsonb) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=r.id AND owner_id=q.quality_task_id AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' OR d.v29_perspective_quality_receipt IS NULL THEN RETURN false;END IF;
 stored:=jsonb_build_object('actor_id',p_actor,'auth_session_id',p_session,'base_version_id',p_version,'decision',p_decision);
 IF d.v29_perspective_apply_decision IS NOT NULL THEN RETURN d.v29_perspective_apply_decision=stored;END IF;
 UPDATE photo.backend_documents SET v29_perspective_apply_decision=stored WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_perspective_apply_decision(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_perspective_apply_decision(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v29_perspective_apply_decision(uuid,uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
