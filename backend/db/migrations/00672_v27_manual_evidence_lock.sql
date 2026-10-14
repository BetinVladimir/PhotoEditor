-- +goose Up
-- Selected manual producer evidence only. Source/auth/version and complete
-- inventory locks remain obligations of the final admission transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_manual_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_mask uuid,p_mask_sha text,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer uuid; actual jsonb; catalog record; pin record; n integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 OR jsonb_typeof(p_evidence) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 n := jsonb_array_length(p_evidence);
 IF n NOT IN(6,7) THEN RETURN false; END IF;
 -- Do not cast untrusted values; match catalog UUIDs through their text form.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(p_evidence) e WHERE jsonb_typeof(e) IS DISTINCT FROM 'object') THEN RETURN false; END IF;
 IF (SELECT count(DISTINCT e->>'role') FROM jsonb_array_elements(p_evidence) e)<>n
 OR (SELECT count(DISTINCT e->>'artifact_id') FROM jsonb_array_elements(p_evidence) e)<>n
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(p_evidence) e WHERE e->>'artifact_id'=p_source::text
 OR coalesce(e->>'role','') NOT IN('rasterized_mask','task_params','mask_target','mask_protected','mask_target_png','mask_protected_png','manual_vectors')) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM unnest(ARRAY['rasterized_mask','task_params','mask_target','mask_protected','mask_target_png','mask_protected_png']) role
 WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_evidence) e WHERE e->>'role'=role)) THEN RETURN false; END IF;
 SELECT created_by_task_id INTO producer FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_mask AND sha256=p_mask_sha;
 IF producer IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=producer ORDER BY attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=producer ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (id=p_source OR created_by_task_id=producer
 OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer)
 OR id::text IN(SELECT e->>'artifact_id' FROM jsonb_array_elements(p_evidence) e)) ORDER BY id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer) FOR SHARE;
 PERFORM run_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id IN(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=producer) ORDER BY run_id FOR SHARE;
 IF (SELECT count(*) FROM photo.semantic_proposal_manual_preparation_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_mask,p_mask_sha))<>1 THEN RETURN false; END IF;
 SELECT * INTO catalog FROM photo.semantic_proposal_manual_preparation_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_mask,p_mask_sha);
 FOR pin IN SELECT a.id,a.sha256,e->>'role' AS role FROM jsonb_array_elements(p_evidence) e JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=e->>'artifact_id' LOOP
  IF pin.role='rasterized_mask' AND (pin.id<>catalog.root_id OR pin.sha256<>catalog.root_sha) THEN RETURN false;
  ELSIF pin.role='task_params' AND (pin.id<>catalog.params_id OR pin.sha256<>catalog.params_sha) THEN RETURN false;
  ELSIF pin.role='mask_target' AND (pin.id<>p_mask OR pin.sha256<>p_mask_sha) THEN RETURN false;
  ELSIF pin.role IN('mask_protected','mask_target_png','mask_protected_png') AND NOT EXISTS(SELECT 1 FROM photo.semantic_proposal_manual_preparation_output_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_mask,p_mask_sha,pin.id,pin.sha256,pin.role)) THEN RETURN false;
  ELSIF pin.role='manual_vectors' AND NOT EXISTS(SELECT 1 FROM photo.semantic_proposal_manual_preparation_vector_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_mask,p_mask_sha,pin.id,pin.sha256)) THEN RETURN false; END IF;
 END LOOP;
 SELECT jsonb_agg(jsonb_build_object('role',e->>'role','artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY e->>'role') INTO actual
 FROM jsonb_array_elements(p_evidence) e JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id::text=e->>'artifact_id';
 RETURN coalesce(actual=p_evidence AND jsonb_array_length(actual)=n,false);
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_manual_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_manual_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_manual_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
