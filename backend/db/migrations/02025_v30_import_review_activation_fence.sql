-- +goose Up
-- The existing activation materializes the exact input closure. This wrapper
-- then checks every review proposal against a live, locked grant in the SAME
-- transaction. Failure raises so the earlier blocked->ready transition rolls
-- back; returning false here would commit an unreviewed ready task.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_interchange_import_review_fenced(
 p_tenant uuid,p_workflow uuid,p_parsed text,p_plan text,p_params text,p_task jsonb,p_reviews jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;t photo.tasks%ROWTYPE;witness jsonb;
 entry_id uuid;target_id uuid;proposal_id uuid;base_id uuid;label_id uuid;
 kind text;proposal_sha text;base_sha text;label_sha text;
 d photo.v30_import_review_decisions%ROWTYPE;seen uuid[]:=ARRAY[]::uuid[];candidate_count bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_reviews IS NULL OR jsonb_typeof(p_reviews) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_reviews)>500 THEN RETURN false;END IF;
 IF photo.activate_v30_interchange_import(p_tenant,p_workflow,p_parsed,p_plan,p_params,p_task) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant AND x.id=p_workflow
  AND x.workflow_type='interchange' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_SCOPE';END IF;
 SELECT * INTO t FROM photo.tasks x WHERE x.tenant_id=p_tenant AND x.run_id=w.run_id
  AND x.id=photo.v30_uuid5(w.id,'v2.interchange.import:0') AND x.state='ready' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_TASK';END IF;
 SELECT count(*) INTO candidate_count FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
  AND i.contract IN('DefectCandidates','DustCandidates');
 IF candidate_count<>jsonb_array_length(p_reviews) THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_COUNT';END IF;
 FOR witness IN SELECT value FROM jsonb_array_elements(p_reviews) LOOP
  IF jsonb_typeof(witness) IS DISTINCT FROM 'object' THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_WITNESS';END IF;
  entry_id:=(witness->>'entry_id')::uuid;
  target_id:=(witness->>'target_version_id')::uuid;
  kind:=witness->>'kind';
  proposal_id:=(witness#>>'{proposal,artifact_id}')::uuid;
  proposal_sha:=witness#>>'{proposal,sha256}';
  base_id:=(witness#>>'{base,artifact_id}')::uuid;
  base_sha:=witness#>>'{base,sha256}';
  label_id:=(witness#>>'{label_map,artifact_id}')::uuid;
  label_sha:=witness#>>'{label_map,sha256}';
  IF entry_id IS NULL OR target_id IS NULL OR proposal_id IS NULL OR base_id IS NULL
  OR kind IS NULL OR kind NOT IN('defects','dust') OR coalesce(proposal_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(base_sha,'') !~ '^[0-9a-f]{64}$'
  OR proposal_id=ANY(seen) THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_IDENTITY';END IF;
  IF kind='defects' AND (label_id IS NULL OR coalesce(label_sha,'') !~ '^[0-9a-f]{64}$')
  OR kind='dust' AND (label_id IS NOT NULL OR label_sha IS NOT NULL) THEN
   RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_LABEL';END IF;
  seen:=array_append(seen,proposal_id);
  PERFORM 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
   AND i.artifact_id=proposal_id AND i.sha256=proposal_sha
   AND i.contract=CASE WHEN kind='defects' THEN 'DefectCandidates' ELSE 'DustCandidates' END;
  IF NOT FOUND THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_PROPOSAL';END IF;
  SELECT * INTO d FROM photo.v30_import_review_decisions x WHERE x.tenant_id=p_tenant
   AND x.workflow_id=p_workflow AND x.project_id=w.project_id AND x.entry_id=entry_id
   AND x.target_version_id=target_id AND x.kind=kind AND x.proposal_id=proposal_id
   AND x.proposal_sha=proposal_sha AND x.base_id=base_id AND x.base_sha=base_sha
   AND x.label_map_id IS NOT DISTINCT FROM label_id AND x.label_map_sha IS NOT DISTINCT FROM label_sha
   AND x.status='granted' FOR SHARE;
  IF NOT FOUND OR d.epoch::text IS DISTINCT FROM witness->>'epoch'
   OR d.decision IS DISTINCT FROM witness->>'decision'
   OR d.accepted_ids IS DISTINCT FROM witness->'accepted_candidate_ids'
   OR d.rejected_ids IS DISTINCT FROM witness->'rejected_candidate_ids'
  THEN RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_GRANT';END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 RAISE EXCEPTION 'V30_IMPORT_REVIEW_ACTIVATION_INVALID';
END $$;
ALTER FUNCTION photo.activate_v30_interchange_import_review_fenced(uuid,uuid,text,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_interchange_import_review_fenced(uuid,uuid,text,text,text,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_interchange_import_review_fenced(uuid,uuid,text,text,text,jsonb,jsonb) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.activate_v30_interchange_import(uuid,uuid,text,text,text,jsonb) FROM photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
