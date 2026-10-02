-- Generic creative candidates use the same immutable review ledger as defect
-- proposals, but freeze an exact candidate/base route without defect subsets.
-- +goose Up
ALTER TABLE photo.reviews DROP CONSTRAINT reviews_decision_route_domain;
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_decision_route_domain CHECK(decision_route IS NULL OR decision_route IN ('repair','candidate','base','skip'));

CREATE UNIQUE INDEX reviews_one_candidate_task ON photo.reviews(tenant_id,task_id)
 WHERE kind<>'defects' AND task_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION photo.create_candidate_review(p_tenant uuid,p_run uuid,p_task uuid,p_base uuid,p_kind text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;candidate photo.artifacts%ROWTYPE;base photo.artifacts%ROWTYPE;
 result_outputs jsonb;existing photo.reviews%ROWTYPE;review_id uuid;
BEGIN
 IF p_kind NOT IN('background_generation','inpaint','generation','composition') THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR t.state<>'succeeded' OR t.operation NOT IN('background.generate','image.inpaint','image.generate','image.compose','geometry.crop')
  OR t.canonical_result_id IS NULL THEN RETURN NULL;END IF;
 SELECT outputs INTO result_outputs FROM photo.task_results WHERE tenant_id=p_tenant AND id=t.canonical_result_id AND task_id=t.id;
 SELECT * INTO candidate FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.state='ready' AND a.output_role='working_image'
  AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF candidate.id IS NULL OR (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.state='ready' AND a.output_role='working_image'
  AND result_outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))<>1 THEN RETURN NULL;END IF;
 SELECT * INTO base FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_base AND a.state='ready' AND a.media_type='application/json'
  AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF base.id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO existing FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task AND kind<>'defects';
 IF FOUND THEN
  IF existing.run_id=p_run AND existing.kind=p_kind AND existing.candidate_artifact_id=candidate.id AND existing.base_artifact_id=base.id AND existing.candidate_hash=candidate.sha256 THEN RETURN existing.id;END IF;
  RETURN NULL;
 END IF;
 IF r.cancel_requested OR r.status NOT IN('running','waiting_review') OR r.revision>=9007199254740991 THEN RETURN NULL;END IF;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons)
  VALUES(p_tenant,p_run,p_task,candidate.id,base.id,candidate.sha256,p_kind,'[]') RETURNING id INTO review_id;
 UPDATE photo.runs SET status='waiting_review',graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN review_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_candidate_review(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_candidate_review(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_candidate_review(uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
