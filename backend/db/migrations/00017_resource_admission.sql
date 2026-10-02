-- +goose Up
CREATE TABLE photo.resource_nodes (
 node_id text PRIMARY KEY CHECK(length(node_id) BETWEEN 1 AND 120),
 enabled boolean NOT NULL DEFAULT false,
 ram_budget_bytes bigint NOT NULL CHECK(ram_budget_bytes>0),
 vram_budget_bytes bigint NOT NULL CHECK(vram_budget_bytes>=0),
 scratch_budget_bytes bigint NOT NULL CHECK(scratch_budget_bytes>0),
 ram_available_bytes bigint NOT NULL DEFAULT 0 CHECK(ram_available_bytes>=0),
 vram_available_bytes bigint NOT NULL DEFAULT 0 CHECK(vram_available_bytes>=0),
 scratch_available_bytes bigint NOT NULL DEFAULT 0 CHECK(scratch_available_bytes>=0),
 reported_at timestamptz,
 doctor_report_sha256 text CHECK(doctor_report_sha256 ~ '^[0-9a-f]{64}$')
);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.resource_nodes TO photo_fn_owner;
ALTER TABLE photo.resource_reservations ADD COLUMN gpu_units integer NOT NULL DEFAULT 0 CHECK(gpu_units IN (0,1));
-- Existing nodes stay disabled until the operator provisions their budgets.
INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes)
 SELECT DISTINCT node_id,1,0,1 FROM photo.resource_slots ON CONFLICT DO NOTHING;
ALTER TABLE photo.resource_slots ADD FOREIGN KEY(node_id) REFERENCES photo.resource_nodes(node_id);

CREATE TABLE photo.operation_registry (
 operation text NOT NULL,version text NOT NULL,resource_class text NOT NULL,routing_key text NOT NULL,
 model_id text,timeout_seconds integer NOT NULL CHECK(timeout_seconds BETWEEN 1 AND 7200),backends jsonb NOT NULL,
 PRIMARY KEY(operation,version)
);
GRANT SELECT ON photo.operation_registry TO photo_fn_owner;
-- Pinned operation catalog from contracts/operations.json; changes need a new migration.
INSERT INTO photo.operation_registry VALUES('asset.verify','1','media','io.verify',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('asset.inspect','1','cpu','cpu.inspect',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('asset.decode','1','cpu','cpu.decode',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('pair.register','1','cpu','cpu.register',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('features.scene','1','gpu','gpu.scene','dinov2_vits14','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('color.stats','1','cpu','cpu.stats',NULL,'60','["cpu"]');
INSERT INTO photo.operation_registry VALUES('mask.person','1','gpu','gpu.person','lraspp_mobilenet_v3_large','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('face.detect','1','gpu','gpu.face_detect','sfd','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('face.landmarks','1','gpu','gpu.landmarks','fan2d','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('pose.detect','1','gpu','gpu.pose','keypointrcnn_resnet50_fpn','300','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('face.parse','1','gpu','gpu.face_parse','bisenet_face19','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('scene.parse','1','gpu','gpu.scene_parse','segformer_b0_ade','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('alpha.predict','1','cpu','cpu.alpha','u2net','300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('mask.prompt','1','gpu','gpu.prompt','mobilesam','180','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('mask.rasterize','1','cpu','cpu.mask_raster',NULL,'60','["cpu"]');
INSERT INTO photo.operation_registry VALUES('mask.transform','1','cpu','cpu.mask_transform',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('alpha.refine','1','cpu','cpu.matting',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('depth.predict','1','gpu','gpu.depth','depth_anything_v2_small','300','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('pair.fit.body','1','cpu','cpu.fit_body',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('pair.fit.color','1','gpu','gpu.fit_color',NULL,'1200','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('pair.fit.beauty','1','cpu','cpu.fit_beauty',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('pair.fit.defects','1','cpu','cpu.fit_defects',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('style.aggregate','1','cpu','cpu.aggregate',NULL,'1200','["cpu"]');
INSERT INTO photo.operation_registry VALUES('style.validate','1','cpu','cpu.style_validate',NULL,'1200','["cpu"]');
INSERT INTO photo.operation_registry VALUES('style.package','1','cpu','cpu.package',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('collection.group','1','cpu','cpu.group',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('collection.cull','1','cpu','cpu.cull',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('style.resolve','1','cpu','cpu.resolve',NULL,'60','["cpu"]');
INSERT INTO photo.operation_registry VALUES('series.measure','1','cpu','cpu.series_measure',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('series.balance','1','cpu','cpu.series_balance',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('image.denoise','1','gpu','gpu.denoise','scunet_color_real_psnr','1800','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('geometry.body','1','cpu','cpu.body',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('image.color','1','cpu','cpu.color',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('image.local','1','cpu','cpu.local',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('portrait.beauty','1','cpu','cpu.beauty',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('portrait.detail','1','cpu','cpu.portrait_detail',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('defects.propose','1','cpu','cpu.defects_propose',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('defects.repair','1','cpu','cpu.defects_repair',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('sensor.dust','1','cpu','cpu.dust',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('composition.plan','1','cpu','cpu.composition',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('geometry.crop','1','cpu','cpu.crop',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('image.inpaint','1','gpu','gpu.inpaint','big_lama','1800','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('image.generate','1','gpu','gpu.generate','sd15_inpaint','7200','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('image.outpaint','1','gpu','gpu.outpaint','sd15_inpaint','7200','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('background.compose','1','cpu','cpu.compose',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('background.blur','1','cpu','cpu.blur',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('background.sky','1','cpu','cpu.sky',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('image.upscale','1','gpu','gpu.upscale','realesrgan','3600','["cpu", "cuda", "mps"]');
INSERT INTO photo.operation_registry VALUES('hdr.register','1','cpu','cpu.hdr_register',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('hdr.merge','1','cpu','cpu.hdr_merge',NULL,'1800','["cpu"]');
INSERT INTO photo.operation_registry VALUES('hdr.window_pull','1','cpu','cpu.window',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('quality.check','1','cpu','cpu.quality',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('media.thumbnail','1','media','io.thumbnail',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('media.preview','1','media','io.preview',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('derivative.resize','1','cpu','cpu.resize',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('derivative.sharpen','1','cpu','cpu.sharpen',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('derivative.watermark','1','cpu','cpu.watermark',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('export.encode','1','cpu','cpu.encode',NULL,'600','["cpu"]');
INSERT INTO photo.operation_registry VALUES('metadata.write','1','cpu','cpu.metadata',NULL,'180','["cpu"]');
INSERT INTO photo.operation_registry VALUES('archive.build','1','archive','io.archive',NULL,'7200','["cpu"]');
INSERT INTO photo.operation_registry VALUES('collection.mutate','1','collection','collection.mutate',NULL,'120','["cpu"]');
INSERT INTO photo.operation_registry VALUES('storage.cleanup','1','media','io.cleanup',NULL,'300','["cpu"]');
INSERT INTO photo.operation_registry VALUES('report.contact_sheet','1','media','io.preview',NULL,'300','["cpu"]');

-- The physical node lock protects aggregate budgets across all its slots.
-- Recheck slot identity after acquiring the task: a concurrent dispatch may
-- have attached a slot after the initial read. In that case retry a fresh tx.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_task(p_task uuid) RETURNS photo.tasks
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; locked photo.tasks%ROWTYPE; initial_slot uuid; current_slot uuid; node text;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT id,node_id INTO initial_slot,node FROM photo.resource_slots WHERE task_id=t.id;
 PERFORM node_id FROM photo.resource_nodes WHERE node_id=node FOR UPDATE;
 PERFORM id FROM photo.resource_slots WHERE task_id=t.id ORDER BY node_id,slot_name FOR UPDATE;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=t.tenant_id FOR UPDATE;
 PERFORM id FROM photo.runs WHERE id=t.run_id AND tenant_id=t.tenant_id FOR UPDATE;
 SELECT * INTO locked FROM photo.tasks WHERE id=t.id AND tenant_id=t.tenant_id FOR UPDATE;
 SELECT id INTO current_slot FROM photo.resource_slots WHERE task_id=t.id;
 IF initial_slot IS DISTINCT FROM current_slot THEN RAISE EXCEPTION 'resource assignment changed' USING ERRCODE='40001'; END IF;
 RETURN locked;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.release_task_resources(p_tenant uuid,p_task uuid,p_fence bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE gpu integer;
BEGIN
 UPDATE photo.resource_slots SET tenant_id=NULL,task_id=NULL,fencing_token=NULL,lease_until=NULL
 WHERE tenant_id=p_tenant AND task_id=p_task AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET state='released',expires_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND task_id=p_task AND fencing_token=p_fence AND state='reserved' RETURNING gpu_units INTO gpu;
 IF FOUND AND gpu>0 THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks-gpu WHERE tenant_id=p_tenant; END IF;
END $$;
-- +goose StatementEnd

-- Heartbeats report measured availability, never raise the operator's budgets.
-- +goose StatementBegin
CREATE FUNCTION photo.report_worker_capacity(p_tenant uuid,p_worker uuid,p_ram bigint,p_vram bigint,p_scratch bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node text;
BEGIN
 IF p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_CAPACITY' USING ERRCODE='22023'; END IF;
 SELECT node_id INTO node FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker AND db_principal=session_user AND enabled;
 IF NOT FOUND THEN RETURN false; END IF;
 PERFORM node_id FROM photo.resource_nodes WHERE node_id=node AND enabled FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 -- Recheck enabled after acquiring the node lock.
 IF NOT EXISTS(SELECT FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker AND db_principal=session_user AND enabled AND node_id=node) THEN RETURN false; END IF;
 UPDATE photo.resource_nodes SET ram_available_bytes=least(p_ram,ram_budget_bytes),vram_available_bytes=least(p_vram,vram_budget_bytes),
  scratch_available_bytes=least(p_scratch,scratch_budget_bytes),reported_at=clock_timestamp() WHERE node_id=node;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=p_tenant AND worker_id=p_worker;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.report_worker_capacity(uuid,uuid,bigint,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.report_worker_capacity(uuid,uuid,bigint,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.report_worker_capacity(uuid,uuid,bigint,bigint,bigint) TO photo_worker;

-- Go computes estimates and chooses candidates; this function is the single
-- authoritative transaction for admission + queue state + durable command.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE; t photo.tasks%ROWTYPE; hint photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; op photo.operation_registry%ROWTYPE; params photo.artifacts%ROWTYPE;
 used_ram numeric; used_vram numeric; used_scratch numeric; used_gpu integer; quota_gpu integer; until_time timestamptz;
 envelope jsonb; model jsonb; backend text; model_lock text; input jsonb; ai photo.artifacts%ROWTYPE;
BEGIN
 IF p_task IS NULL OR p_event IS NULL OR p_correlation IS NULL OR p_node IS NULL OR p_trace IS NULL OR length(p_trace)>128
 OR p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<=0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_RESERVATION' USING ERRCODE='22023'; END IF;
 SELECT * INTO hint FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb; RETURN; END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT n.enabled OR n.reported_at IS NULL OR n.reported_at<clock_timestamp()-interval '30 seconds' OR n.doctor_report_sha256 IS NULL THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node AND resource_class=hint.resource_class AND task_id IS NULL ORDER BY slot_name LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_USAGE_MISSING'; END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF t.state<>'ready' OR t.attempt>=6 OR t.tenant_id<>hint.tenant_id OR t.run_id<>hint.run_id OR t.resource_class<>slot.resource_class THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 IF t.cancel_requested OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN
 RETURN QUERY SELECT 'cancelled',NULL::jsonb; RETURN; END IF;
 IF NOT r.graph_sealed THEN RETURN QUERY SELECT 'unsealed',NULL::jsonb; RETURN; END IF;
 SELECT * INTO op FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version;
 IF NOT FOUND OR op.resource_class<>t.resource_class THEN RAISE EXCEPTION 'OPERATION_MISMATCH' USING ERRCODE='22023'; END IF;
 backend:=t.execution_profile->>'backend';
 IF backend IS NULL OR NOT op.backends ? backend THEN RETURN QUERY SELECT 'unsupported_backend',NULL::jsonb; RETURN; END IF;
 model:=NULL;
 IF op.model_id IS NOT NULL THEN
  model_lock:=t.execution_profile->>'model_lock_id';
  IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;
  model:=jsonb_build_object('id',op.model_id,'lock_id',model_lock);
 END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled
 AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class AND w.backends ? backend AND (w.qualified_operations->t.operation) ? backend
 AND w.last_seen>clock_timestamp()-interval '30 seconds' AND (op.model_id IS NULL OR w.lock_id=model_lock)) THEN
 RETURN QUERY SELECT 'waiting_worker',NULL::jsonb; RETURN; END IF;
 SELECT coalesce(sum(ram_bytes),0),coalesce(sum(vram_bytes),0),coalesce(sum(scratch_bytes),0),coalesce(sum(gpu_units),0)
 INTO used_ram,used_vram,used_scratch,used_gpu FROM photo.resource_reservations WHERE node_id=p_node AND state='reserved';
 IF used_ram+p_ram>least(n.ram_budget_bytes,n.ram_available_bytes) OR used_vram+p_vram>least(n.vram_budget_bytes,n.vram_available_bytes)
 OR used_scratch+p_scratch>least(n.scratch_budget_bytes,n.scratch_available_bytes) OR (t.resource_class='gpu' AND used_gpu>=1) THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT coalesce((quotas->>'max_active_gpu_tasks')::integer,1) INTO quota_gpu FROM photo.tenants WHERE id=t.tenant_id;
 IF t.resource_class='gpu' AND (SELECT active_gpu_tasks>=quota_gpu FROM photo.tenant_usage WHERE tenant_id=t.tenant_id) THEN
 RETURN QUERY SELECT 'waiting_quota',NULL::jsonb; RETURN; END IF;
 -- Artifact locks are acquired after task, in UUID order, including params.
 IF jsonb_typeof(t.inputs)<>'array' OR jsonb_array_length(t.inputs)>128 THEN RAISE EXCEPTION 'INVALID_INPUTS'; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(input)<>'object' OR (input-'artifact_id'-'sha256')<>'{}' OR input->>'artifact_id' IS NULL OR input->>'sha256' IS NULL OR input->>'sha256' !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INPUTS' USING ERRCODE='22023'; END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=t.tenant_id AND task_id=t.id AND state='reserved') THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs || jsonb_build_array(jsonb_build_object('artifact_id',t.params_artifact_id))) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO ai FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(input->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR ai.state<>'ready' OR (ai.expires_at IS NOT NULL AND ai.expires_at<=clock_timestamp()) OR
   (input ? 'sha256' AND ai.sha256 IS DISTINCT FROM input->>'sha256') THEN RETURN QUERY SELECT 'input_unavailable',NULL::jsonb; RETURN; END IF;
  IF ai.id=t.params_artifact_id THEN params:=ai; END IF;
 END LOOP;
 envelope:=jsonb_build_object('schema_version',1,'event_id',p_event,'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,
 'attempt',t.attempt+1,'operation',t.operation,'operation_version',t.operation_version,'resource_class',t.resource_class,'correlation_id',p_correlation,
 'inputs',t.inputs,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),'model',model,'timeout_seconds',op.timeout_seconds,'traceparent',p_trace);
 IF octet_length(envelope::text)>65536 THEN RAISE EXCEPTION 'COMMAND_TOO_LARGE' USING ERRCODE='22023'; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,state,expires_at,fencing_token,gpu_units)
 VALUES(t.tenant_id,t.id,p_node,p_scratch,p_ram,p_vram,'reserved',until_time,t.fencing_token,CASE WHEN t.resource_class='gpu' THEN 1 ELSE 0 END)
 ON CONFLICT(tenant_id,task_id) DO UPDATE SET node_id=excluded.node_id,scratch_bytes=excluded.scratch_bytes,ram_bytes=excluded.ram_bytes,vram_bytes=excluded.vram_bytes,
 state='reserved',expires_at=excluded.expires_at,fencing_token=excluded.fencing_token,gpu_units=excluded.gpu_units;
 UPDATE photo.resource_slots SET task_id=t.id,tenant_id=t.tenant_id,fencing_token=t.fencing_token,lease_until=until_time WHERE id=slot.id;
 IF t.resource_class='gpu' THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks+1 WHERE tenant_id=t.tenant_id; END IF;
 UPDATE photo.tasks SET state='queued',dispatch_command=envelope,timeout_seconds=op.timeout_seconds,terminal_reason=NULL WHERE id=t.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,p_event,'photo.commands.v1',op.routing_key,envelope);
 RETURN QUERY SELECT 'queued',envelope;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
