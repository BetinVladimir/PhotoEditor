-- +goose Up
-- Candidate discovery only. Physical original-version reads remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_negative_adopted_replacements(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_nodes jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb;original uuid;task uuid;run uuid;result jsonb:='[]'::jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' OR pg_column_size(p_nodes)>1048576
 OR p_review IS NULL OR coalesce(p_hash,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 IF jsonb_array_length(p_nodes)>4096 THEN RETURN NULL;END IF;
 IF (SELECT count(DISTINCT value->>'original_task_id') FROM jsonb_array_elements(p_nodes))<>jsonb_array_length(p_nodes)
 OR (SELECT count(DISTINCT value->>'task_id') FROM jsonb_array_elements(p_nodes))<>jsonb_array_length(p_nodes) THEN RETURN NULL;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) ORDER BY value->>'original_task_id' LOOP
 IF jsonb_typeof(node) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(node) k) IS DISTINCT FROM ARRAY['original_task_id','run_id','task_id']::text[] THEN RETURN NULL;END IF;
 original:=(node->>'original_task_id')::uuid;task:=(node->>'task_id')::uuid;run:=(node->>'run_id')::uuid;
 IF original IS NULL OR task IS NULL OR run IS NULL
 OR original::text IS DISTINCT FROM node->>'original_task_id' OR task::text IS DISTINCT FROM node->>'task_id' OR run::text IS DISTINCT FROM node->>'run_id'
 OR photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,original) IS NULL THEN RETURN NULL;END IF;
 IF photo.v29_negative_replacement_member(p_tenant,p_project,p_actor,p_session,p_review,p_hash,original,task,run)
 AND photo.read_v29_adopted_optical_parent(p_tenant,p_project,p_actor,p_session,task,run) IS NOT NULL THEN result:=result||jsonb_build_array(node);END IF;
 END LOOP;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_negative_adopted_replacements(uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_negative_adopted_replacements(uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_negative_adopted_replacements(uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
