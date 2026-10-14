-- +goose Up
-- Authenticate the proposed target, exact selected root bytes and root/binary
-- canonical producer. This is not numerical NPY or qualification evidence.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_protection_origin(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_target uuid,p_target_revision bigint,p_version uuid,p_candidate uuid,p_candidate_sha text,p_root uuid,p_root_sha text,p_binary uuid,p_binary_sha text,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE metadata record;project uuid;source uuid;source_sha text;producer uuid;body jsonb;
BEGIN
 SELECT * INTO metadata FROM photo.semantic_approved_protection_pin(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha);
 IF metadata.artifact_id IS DISTINCT FROM p_root OR metadata.sha256 IS DISTINCT FROM p_root_sha THEN RETURN false;END IF;
 SELECT i.project_id INTO project FROM photo.semantic_intents i WHERE i.tenant_id=p_tenant AND i.id=p_intent;
 SELECT v.master_artifact_id,v.content_hash INTO source,source_sha FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=project
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND a.deleted_at IS NULL AND a.upload_state='verified' FOR SHARE OF v,a;
 IF source IS NULL THEN RETURN false;END IF;
 SELECT a.created_by_task_id INTO producer FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_root;
 PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=producer FOR SHARE;
 PERFORM r.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=producer FOR SHARE OF r;
 PERFORM r.id FROM photo.task_results r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.canonical_result_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=producer FOR SHARE OF r;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(source,p_root,p_binary) ORDER BY a.id FOR SHARE;
 SELECT * INTO metadata FROM photo.semantic_approved_protection_object(p_tenant,p_intent,p_actor,p_session,p_revision,p_target,p_target_revision,p_version,p_candidate,p_candidate_sha,p_root,p_root_sha);
 IF metadata.artifact_id IS NULL OR p_body IS NULL OR octet_length(p_body)<>metadata.bytes
 OR encode(sha256(convert_to(p_body,'UTF8')),'hex') IS DISTINCT FROM p_root_sha THEN RETURN false;END IF;
 body:=p_body::jsonb;
 IF body->'protected' IS DISTINCT FROM 'true'::jsonb
 OR body->'artifact' IS DISTINCT FROM jsonb_build_object('artifact_id',p_binary,'sha256',p_binary_sha) THEN RETURN false;END IF;
 RETURN photo.semantic_protection_producer_ready(p_tenant,project,source,source_sha,p_root,p_root_sha,p_binary,p_binary_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_protection_origin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_protection_origin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_approved_protection_origin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
