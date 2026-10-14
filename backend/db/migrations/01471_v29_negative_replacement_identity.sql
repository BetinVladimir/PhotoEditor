-- +goose Up
-- Resolve only a persisted original creator/session which is STILL an editor.
-- This does not create, renew or impersonate a session; no access token is read.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_replacement_identity(p_tenant uuid,p_project uuid,p_review uuid,p_hash text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actor uuid;session uuid;config jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 SELECT r.created_by,r.config_snapshot INTO actor,config FROM photo.reviews v
 JOIN photo.tasks t ON t.tenant_id=v.tenant_id AND t.id=v.task_id AND t.run_id=v.run_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE v.tenant_id=p_tenant AND v.id=p_review AND w.project_id=p_project
 AND v.kind='optical.negative' AND v.state='decided' AND v.revision=2
 AND v.decision='reject' AND v.decision_route='base' AND v.decision_hash=p_hash
 AND t.operation='optical.negative' AND t.operation_version='1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF config ? 'v29_negative_replacement_draft_jcs' THEN
 session:=((config->>'v29_negative_replacement_draft_jcs')::jsonb->'owner'->>'auth_session_id')::uuid;
 ELSE session:=(config->'v29_negative'->>'auth_session_id')::uuid;END IF;
 IF actor IS NULL OR session IS NULL OR photo.v2_editor_authorized(p_tenant,p_project,actor,session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('actor_id',actor,'auth_session_id',session);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_negative_replacement_identity(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_replacement_identity(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_replacement_identity(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
