-- +goose Up
-- Catalog only; deployment qualification must explicitly enable execution.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES
('image.relight','1','cpu','cpu.image_relight',NULL,600,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('image.relight','1','document','["application/json"]',8388608,1),
('image.relight','1','working_image','["application/json"]',8388608,1),
('image.relight','1','pixels','["application/x-npy"]',2400004096,1),
('image.relight','1','report','["application/json"]',8388608,1),
('image.relight','1','mask','["application/json"]',8388608,2),
('image.relight','1','mask_pixels','["application/x-npy"]',800004096,2),
('image.relight','1','transform','["application/json"]',8388608,1),
('image.relight','1','registration','["application/json"]',8388608,30),
('image.relight','1','alpha','["application/x-npy"]',800004096,1),
('image.relight','1','warp_field','["application/x-npy"]',1600004096,32);

ALTER TABLE photo.runs ADD COLUMN v28_relight_queued_receipt jsonb
 CHECK(v28_relight_queued_receipt IS NULL OR (jsonb_typeof(v28_relight_queued_receipt)='object' AND pg_column_size(v28_relight_queued_receipt)<=65536));
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v28_relight_graph(p_tenant uuid,p_run uuid,p_task uuid,p_descriptor text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;workflow_id uuid;transport jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 -- Exclusive owner locks precede all subsidiary dependency locks.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.v28_relight_queued_receipt IS NOT NULL OR r.v28_relight_descriptor_jcs IS DISTINCT FROM p_descriptor
 OR r.v28_relight_intent IS NULL THEN RETURN false;END IF;
 SELECT id INTO workflow_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
 AND input_snapshot_id=r.input_snapshot_id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run) THEN RETURN false;END IF;
 transport:=photo.read_v28_relight_registered_transport(p_tenant,p_run,p_task,p_descriptor,p_proof);
 IF transport IS NULL THEN RETURN false;END IF;
 -- Retain every generic registry/output-policy/closure/quota/activation gate;
 -- only the dedicated relight denial wrapper is bypassed after live authority.
 IF photo.seal_v2_graph_pre_relight_admission(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.runs SET v28_relight_queued_receipt=jsonb_build_object('schema_version',1,
 'run_id',p_run,'workflow_id',workflow_id,'task_id',p_task,'state','queued',
 'actor_id',r.v28_relight_intent->>'actor_id','auth_session_id',r.v28_relight_intent->>'auth_session_id',
 'descriptor_sha',encode(sha256(convert_to(p_descriptor,'UTF8')),'hex'),
 'qualification',r.v28_relight_intent->'qualification','transport',transport)
 WHERE tenant_id=p_tenant AND id=p_run AND graph_sealed AND v28_relight_queued_receipt IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'V28_RELIGHT_RECEIPT_RACE' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.seal_v28_relight_graph(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v28_relight_graph(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v28_relight_graph(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
