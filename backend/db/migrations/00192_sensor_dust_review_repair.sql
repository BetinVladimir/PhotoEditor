-- Bind recurrent sensor dust proposals to review and register their dedicated repair.
-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
VALUES('sensor.dust_repair','1','cpu','cpu.dust_repair',NULL,300,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('sensor.dust_repair','1','working_rgb','["application/octet-stream"]',12884901888,1),
 ('sensor.dust_repair','1','working_image','["application/json"]',1048576,1),
 ('sensor.dust_repair','1','dust_repair_report','["application/json"]',1048576,1);

-- +goose StatementBegin
CREATE FUNCTION photo.create_dust_review(p_tenant uuid,p_run uuid,p_task uuid,p_base uuid)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;proposal photo.artifacts%ROWTYPE;base photo.artifacts%ROWTYPE;
 outputs jsonb;existing photo.reviews%ROWTYPE;review_id uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR t.operation<>'sensor.dust' OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.canonical_result_id IS NULL THEN RETURN NULL;END IF;
 SELECT z.outputs INTO outputs FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=t.canonical_result_id AND z.task_id=t.id;
 IF outputs IS NULL OR (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.state='ready' AND a.output_role='dust_candidates'
  AND outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256)))<>1 THEN RETURN NULL;END IF;
 SELECT * INTO proposal FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=t.id AND a.state='ready' AND a.output_role='dust_candidates'
  AND outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 SELECT * INTO base FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_base AND a.state='ready' AND a.media_type='application/json'
  AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256));
 IF base.id IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO existing FROM photo.reviews WHERE tenant_id=p_tenant AND task_id=p_task AND kind<>'defects';
 IF FOUND THEN
  IF existing.run_id=p_run AND existing.kind='sensor_dust' AND existing.candidate_artifact_id=proposal.id AND existing.base_artifact_id=base.id AND existing.candidate_hash=proposal.sha256 THEN RETURN existing.id;END IF;
  RETURN NULL;
 END IF;
 IF r.cancel_requested OR r.status NOT IN('running','waiting_review') OR r.revision>=9007199254740991 THEN RETURN NULL;END IF;
 INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons)
 VALUES(p_tenant,p_run,p_task,proposal.id,base.id,proposal.sha256,'sensor_dust','[]') RETURNING id INTO review_id;
 UPDATE photo.runs SET status='waiting_review',graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN review_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_dust_review(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_dust_review(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_dust_review(uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
