-- +goose Up
-- Exact selected skin/parser lineage under retained producer locks; does not
-- establish root bytes/scientific coverage or grant queue activation.
-- +goose StatementBegin
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_skin;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_skin(uuid,uuid,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;actor uuid;session_id uuid;version uuid;source uuid;source_sha text;origin jsonb;root jsonb;entry jsonb;mask jsonb;
 producer photo.tasks%ROWTYPE;matches integer;matched text;used text[]:='{}';total integer;manifests integer;pixels integer;
BEGIN
 IF photo.lock_semantic_defect_admission_evidence_pre_skin(p_tenant,p_run,p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 project:=(p_evidence->>'project_id')::uuid;actor:=(p_evidence->>'actor_id')::uuid;session_id:=(p_evidence->>'session_id')::uuid;
 version:=(p_evidence->'source_authority'->'binding'->>'asset_version_id')::uuid;
 source:=(p_evidence->'content'->'working_image'->>'artifact_id')::uuid;source_sha:=p_evidence->'content'->'working_image'->>'sha256';origin:=p_evidence->'origin_refs';
 FOR root IN SELECT value FROM jsonb_array_elements(p_evidence->'parser'->'roots') LOOP
  mask:=root->'Binding'->'Mask';IF mask->>'selector'<>'skin' THEN CONTINUE;END IF;
  SELECT t.* INTO producer FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
  WHERE a.tenant_id=p_tenant AND a.id=(mask->'artifact'->>'artifact_id')::uuid AND a.sha256=mask->'artifact'->>'sha256' FOR SHARE OF a,t;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=producer.run_id FOR SHARE;
  PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id FOR SHARE;
  PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=producer.id AND attempt=producer.attempt AND fencing_token=producer.fencing_token FOR SHARE;
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (id=producer.params_artifact_id OR created_by_task_id=producer.id) ORDER BY id FOR SHARE;
  IF (SELECT count(*) FROM photo.semantic_proposal_defect_face_mask_objects(p_tenant,project,actor,session_id,version,source,source_sha,
   (origin->0->>'artifact_id')::uuid,origin->0->>'sha256',(origin->1->>'artifact_id')::uuid,origin->1->>'sha256',(origin->2->>'artifact_id')::uuid,origin->2->>'sha256',
   (origin->3->>'artifact_id')::uuid,origin->3->>'sha256',(origin->4->>'artifact_id')::uuid,origin->4->>'sha256',
   (mask->'artifact'->>'artifact_id')::uuid,mask->'artifact'->>'sha256','skin') object
   WHERE object.root_id=(root->'Root'->>'artifact_id')::uuid AND object.root_sha=root->'Root'->>'sha256')<>1 THEN RETURN false;END IF;
  matches:=0;matched:=NULL;
  FOR entry IN SELECT value FROM jsonb_array_elements(p_evidence->'parser'->'parsers') LOOP
   SELECT count(*),count(*) FILTER(WHERE object.contract='manifest' AND object.id=(entry->'labels'->>'artifact_id')::uuid AND object.sha=entry->'labels'->>'sha256'),
    count(*) FILTER(WHERE object.contract='pixels' AND object.id=(entry->'pixels'->>'artifact_id')::uuid AND object.sha=entry->'pixels'->>'sha256') INTO total,manifests,pixels
   FROM photo.semantic_proposal_defect_skin_parse_objects(p_tenant,project,actor,session_id,version,source,source_sha,
    (origin->0->>'artifact_id')::uuid,origin->0->>'sha256',(origin->1->>'artifact_id')::uuid,origin->1->>'sha256',(origin->2->>'artifact_id')::uuid,origin->2->>'sha256',
    (origin->3->>'artifact_id')::uuid,origin->3->>'sha256',(origin->4->>'artifact_id')::uuid,origin->4->>'sha256',
    (mask->'artifact'->>'artifact_id')::uuid,mask->'artifact'->>'sha256','skin',(entry->'labels'->>'artifact_id')::uuid,entry->'labels'->>'sha256') object;
   IF total=3 AND manifests=1 AND pixels=1 THEN matches:=matches+1;matched:=entry->'labels'->>'artifact_id';END IF;
  END LOOP;
  IF matches<>1 THEN RETURN false;END IF;
  IF NOT matched=ANY(used) THEN used:=array_append(used,matched);END IF;
 END LOOP;
 IF cardinality(used)<>jsonb_array_length(p_evidence->'parser'->'parsers') THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,project,actor,session_id,version,source,source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
