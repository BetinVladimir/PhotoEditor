-- +goose Up
-- Atomic export/item adoption after independent physical verification. Package
-- publication and run quota/finalization stay in their dedicated later paths.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_delivery_encode(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_source text,p_manifest text,p_raster_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;x photo.exports%ROWTYPE;
 manifest jsonb;request jsonb;binary_id uuid;marker uuid;next_cursor bigint;parent_id uuid;root_id uuid;
BEGIN
 IF p_manifest IS NULL OR octet_length(p_manifest) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 manifest:=p_manifest::jsonb;binary_id:=(manifest->'artifact'->>'artifact_id')::uuid;
 marker:=photo.v2_adoption_event_id(p_task,p_result);
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF r.id IS NULL THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';parent_id:=(request->>'parent_run_id')::uuid;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=parent_id;
 IF NOT FOUND OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(r.id,parent_id) ORDER BY id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF t.operation<>'export.encode' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result OR t.state<>'succeeded' OR t.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND workflow_id=w.id FOR UPDATE;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=p_tenant AND package_id=d.id
 AND ordinal=(request->>'ordinal')::integer AND preset_id=(request->'preset'->>'preset_id')::uuid FOR UPDATE;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=p_tenant AND id=i.export_id FOR UPDATE;
 IF d.id IS NULL OR i.id IS NULL OR x.id IS NULL OR x.task_id IS DISTINCT FROM t.id
 OR x.id IS DISTINCT FROM (r.config_snapshot->>'export_id')::uuid THEN RETURN false;END IF;
 SELECT a.id INTO root_id FROM photo.task_results z CROSS JOIN LATERAL jsonb_array_elements(z.outputs) output
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid
 WHERE z.tenant_id=p_tenant AND z.task_id=p_task AND z.id=p_result AND a.output_role='encoded_manifest'
 AND a.sha256=output->>'sha256' AND a.sha256=encode(sha256(convert_to(p_manifest,'UTF8')),'hex')
 AND a.bytes=octet_length(p_manifest);
 IF root_id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
 AND a.sha256=encode(sha256(convert_to(p_params,'UTF8')),'hex') AND a.bytes=octet_length(p_params)) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=marker) THEN
  RETURN i.state='ready' AND x.status='ready' AND x.artifact_id=binary_id
  AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified' AND event_id=marker);
 END IF;
 IF d.state<>'building' OR i.state<>'pending' OR x.status<>'queued' OR x.artifact_id IS NOT NULL THEN RETURN false;END IF;
 IF NOT photo.record_v30_delivery_encode_verification(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_source,p_manifest,p_raster_bytes) THEN RETURN false;END IF;
 -- Retain the complete already sealed input inventory and both physical outputs
 -- on the parent workflow. Task-specific purpose prevents cross-slot collisions.
 INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
 SELECT p_tenant,w.project_id,w.id,q.artifact_id,q.sha256,'delivery.encode:'||p_task::text,
 (row_number() OVER(ORDER BY q.artifact_id)-1)::integer FROM (
 SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task
 UNION SELECT a.id,a.sha256 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(root_id,binary_id)) q;
 UPDATE photo.exports SET artifact_id=binary_id,status='ready' WHERE tenant_id=p_tenant AND id=x.id;
 UPDATE photo.delivery_items SET state='ready',revision=revision+1,updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND id=i.id AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_ITEM_REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(p_tenant,'v2-adopt',marker,clock_timestamp());
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991
 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,r.id,r.created_by,'principal','v2.task.adopted',jsonb_build_object('task_id',p_task,'result_id',p_result,
 'attempt',p_attempt,'fence',p_fence,'export_id',x.id,'delivery_item_id',i.id));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,marker,'photo.events.v1','v2.task.adopted',jsonb_build_object('schema_version',1,'event_id',marker,
 'type','v2.task.adopted','tenant_id',p_tenant,'run_id',r.id,'task_id',p_task,'result_id',p_result));
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_delivery_encode(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
