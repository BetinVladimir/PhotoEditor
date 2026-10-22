-- +goose Up
-- Worker provenance lookup requires the exact live retouch task lease.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_worker_landmark_origin(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text,p_landmark uuid,p_landmark_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;project uuid;face jsonb;
BEGIN
 IF photo.admit_semantic_retouch_preview(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT config_snapshot->'semantic_retouch_preview' INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=(p_body::jsonb->>'intent_id')::uuid;
 IF config->'adapter_context'->'source' IS DISTINCT FROM p_body::jsonb->'source'
 OR jsonb_typeof(config->'adapter_context'->'faces') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT value INTO face FROM jsonb_array_elements(config->'adapter_context'->'faces')
 WHERE value->'landmarks'=jsonb_build_object('artifact_id',p_landmark,'sha256',p_landmark_sha) LIMIT 1;
 IF face IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
 AND i.project_id=project AND i.task_id=p_task AND i.artifact_id=p_landmark AND i.sha256=p_landmark_sha
 AND i.contract=CASE WHEN face->>'object_id' IS NULL THEN 'Face68' ELSE 'FaceLandmarks' END) THEN RETURN false;END IF;
 RETURN photo.semantic_retouch_preview_landmark_origin(p_tenant,project,(config->>'actor_id')::uuid,
 (config->>'session_id')::uuid,p_body::jsonb->'source',p_landmark,p_landmark_sha);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
