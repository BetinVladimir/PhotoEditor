-- +goose Up
-- Bind the already verified upload to the composition task's stable document.
-- This predicate does not create tasks or authorize proposal/mask lineage.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_task_params_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_task uuid,p_artifact uuid,p_sha text,p_params_jcs text,
 p_width bigint,p_height bigint,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE h bytea; document uuid;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 IF NOT photo.v25_layer_original_params_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,
  p_artifact,p_sha,p_params_jcs,p_width,p_height,p_profile) THEN RETURN false;END IF;
 h:=public.digest(uuid_send(p_task)||convert_to('layer.compose.params:1','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 document:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant
  AND u.project_id=p_project AND u.document_id=document AND u.artifact_id=p_artifact
  AND u.sha256=p_sha AND u.state='ready' AND u.completed_at IS NOT NULL);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_task_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_task_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_task_params_scope(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
