-- +goose Up
-- One immutable decision context per canonical proposal task.
CREATE UNIQUE INDEX reviews_one_defect_task ON photo.reviews(tenant_id,task_id)
 WHERE kind='defects' AND task_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION photo.create_defect_review(p_tenant uuid,p_run uuid,p_task uuid,p_image uuid,p_resolved uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; t photo.tasks%ROWTYPE; existing photo.reviews%ROWTYPE;
 proposal photo.artifacts%ROWTYPE; report photo.artifacts%ROWTYPE;
 image photo.artifacts%ROWTYPE; resolved photo.artifacts%ROWTYPE;
 result_outputs jsonb; review_id uuid;
BEGIN
 -- Global order matches cancellation, decision and expansion: run before child.
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR t.operation<>'defects.propose' OR t.operation_version<>'1' OR t.state<>'succeeded' THEN RETURN NULL; END IF;
 SELECT outputs INTO result_outputs FROM photo.task_results
 WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 -- Roles are single outputs in the operation policy. Require exactly one here
 -- as well, so malformed persisted data cannot choose an arbitrary candidate.
 IF (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task
  AND a.state='ready' AND a.output_role='defect_candidates'
  AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))<>1
 OR (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task
  AND a.state='ready' AND a.output_role='defect_propose_report'
  AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))<>1 THEN RETURN NULL; END IF;
 SELECT * INTO proposal FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task
 AND a.state='ready' AND a.output_role='defect_candidates'
 AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 SELECT * INTO report FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=p_task
 AND a.state='ready' AND a.output_role='defect_propose_report'
 AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 SELECT * INTO image FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_image AND a.state='ready'
 AND a.media_type='application/json'
 AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT a.* INTO resolved FROM photo.artifacts a
 JOIN photo.tasks st ON st.tenant_id=a.tenant_id AND st.id=a.created_by_task_id AND st.run_id=p_run
 JOIN photo.task_results sr ON sr.tenant_id=st.tenant_id AND sr.id=st.canonical_result_id AND sr.task_id=st.id
 WHERE a.tenant_id=p_tenant AND a.id=p_resolved AND a.state='ready' AND a.output_role='resolved_style'
 AND st.operation='style.resolve' AND st.operation_version='1' AND st.state='succeeded'
 AND sr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO existing FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task AND kind='defects';
 IF FOUND THEN
  IF existing.run_id=p_run AND existing.candidate_artifact_id=proposal.id AND existing.candidate_hash=proposal.sha256
   AND existing.base_artifact_id=p_image AND existing.resolved_artifact_id=p_resolved
   AND existing.proposal_report_artifact_id=report.id THEN RETURN existing.id; END IF;
  RETURN NULL;
 END IF;
 IF r.cancel_requested OR r.status NOT IN ('running','waiting_review') OR r.revision>=9007199254740991 THEN RETURN NULL; END IF;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons,
  resolved_artifact_id,proposal_report_artifact_id)
 VALUES(p_tenant,p_run,p_task,proposal.id,p_image,proposal.sha256,'defects','[]',p_resolved,report.id)
 RETURNING id INTO review_id;
 UPDATE photo.runs SET status='waiting_review',graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN review_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_defect_review(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_defect_review(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_defect_review(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
