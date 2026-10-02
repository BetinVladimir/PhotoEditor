-- +goose Up
-- Bind each full child plan to its immutable compact parent before quota/staging.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v26_render_batch_part(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,
 p_ordinal integer,p_config_jcs text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; part jsonb; config jsonb; child uuid; workflow uuid; h bytea;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_config_jcs IS NULL
  OR octet_length(p_config_jcs) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.kind<>'bulk' OR parent.status<>'draft' OR parent.cancel_requested OR parent.graph_sealed
  OR parent.created_by IS DISTINCT FROM p_actor
  OR parent.config_snapshot->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR parent.config_snapshot->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR jsonb_typeof(parent.config_snapshot->'parts') IS DISTINCT FROM 'array'
  OR p_ordinal>=jsonb_array_length(parent.config_snapshot->'parts') THEN RETURN false;END IF;
 part:=parent.config_snapshot->'parts'->p_ordinal;config:=p_config_jcs::jsonb;child:=(part->>'run_id')::uuid;
 IF encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex') IS DISTINCT FROM part->>'config_sha'
  OR jsonb_typeof(config->'v26_render'->'nodes') IS DISTINCT FROM 'array'
  OR jsonb_array_length(config->'v26_render'->'nodes') IS DISTINCT FROM (part->>'frame_count')::integer THEN RETURN false;END IF;
 h:=public.digest(uuid_send(child)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 RETURN photo.begin_v26_render_draft(p_tenant,(parent.config_snapshot->>'project_id')::uuid,
  parent.input_snapshot_id,p_actor,p_session,child,workflow,p_config_jcs,
  parent.config_snapshot->>'policy_sha',(part->>'output_bytes')::bigint);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v26_render_batch_part(uuid,uuid,uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v26_render_batch_part(uuid,uuid,uuid,uuid,integer,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v26_render_batch_part(uuid,uuid,uuid,uuid,integer,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_part_state(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_ordinal integer)
RETURNS TABLE(run_id uuid,workflow_id uuid,project_id uuid,snapshot_id uuid,config jsonb,config_sha text,policy_sha text,reserved_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT (part.body->>'run_id')::uuid,child.workflow_id,child.project_id,child.snapshot_id,child.config,
  child.config_sha,child.policy_sha,child.reserved_bytes
 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session) parent
 CROSS JOIN LATERAL (SELECT parent.manifest->'parts'->p_ordinal AS body) part
 CROSS JOIN LATERAL photo.v26_render_draft_state(p_tenant,(part.body->>'run_id')::uuid,p_actor,p_session) child
 WHERE p_ordinal BETWEEN 0 AND 4999 AND NOT parent.cancel_requested
  AND child.project_id=(parent.manifest->>'project_id')::uuid
  AND child.snapshot_id=(parent.manifest->>'input_snapshot_id')::uuid
  AND child.config_sha=part.body->>'config_sha' AND child.policy_sha=parent.manifest->>'policy_sha'
  AND child.reserved_bytes=(part.body->>'output_bytes')::bigint
  AND jsonb_array_length(child.config->'v26_render'->'nodes')=(part.body->>'frame_count')::integer;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_part_state(uuid,uuid,uuid,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_part_state(uuid,uuid,uuid,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_batch_part_state(uuid,uuid,uuid,uuid,integer) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
