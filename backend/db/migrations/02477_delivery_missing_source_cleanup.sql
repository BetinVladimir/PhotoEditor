-- +goose Up
-- Catalogue loss can prevent expansion claiming itself. Reconcile independently
-- of claim readiness; never infer physical absence from a network exception.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_next_v30_delivery_missing_source(p_instance uuid,p_bucket text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint record;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 t photo.tasks%ROWTYPE;d photo.delivery_packages%ROWTYPE;event_id uuid;next_cursor bigint;
BEGIN
 IF p_instance IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR NOT EXISTS(
  SELECT FROM photo.v30_delivery_runtime_leases WHERE instance_id=p_instance AND bucket=p_bucket
  AND expires_at>clock_timestamp()) THEN RETURN false;END IF;
 FOR hint IN SELECT x.id,x.tenant_id,x.run_id FROM photo.graph_expansions x
 JOIN photo.runs run ON run.tenant_id=x.tenant_id AND run.id=x.run_id
 JOIN photo.backend_documents b ON b.tenant_id=x.tenant_id AND b.run_id=x.run_id
  AND b.owner_id=x.parent_task_id AND b.purpose='task_params' AND b.bucket=p_bucket
 WHERE x.kind='v2.delivery/1' AND x.state='open' AND run.status='running'
 AND NOT run.graph_sealed AND NOT run.cancel_requested
 AND (x.expansion_lease_until IS NULL OR x.expansion_lease_until<=clock_timestamp())
 AND EXISTS(SELECT FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=x.tenant_id AND i.task_id=x.parent_task_id AND
  (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
   OR a.object_version IS NULL OR a.object_version IN('','null')
   OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())))
 ORDER BY x.created_at,x.id LIMIT 100 LOOP
  PERFORM id FROM photo.tenants WHERE id=hint.tenant_id AND status='active' FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN CONTINUE;END IF;
  SELECT * INTO r FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested THEN CONTINUE;END IF;
  SELECT * INTO e FROM photo.graph_expansions WHERE id=hint.id AND tenant_id=r.tenant_id AND run_id=r.id FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR e.state<>'open' OR e.expansion_lease_until>clock_timestamp() THEN CONTINUE;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=e.parent_task_id AND run_id=e.run_id FOR SHARE;
  IF NOT FOUND OR t.operation<>'v2.delivery.prepare' OR t.operation_version<>'1' OR t.state<>'succeeded'
   OR t.cancel_requested OR NOT EXISTS(SELECT FROM photo.task_results z WHERE z.tenant_id=t.tenant_id
    AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token)
   OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=t.tenant_id AND consumer='v2-adopt'
    AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN CONTINUE;END IF;
  IF NOT EXISTS(SELECT FROM photo.backend_documents b WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id
   AND b.owner_id=t.id AND b.artifact_id=t.params_artifact_id AND b.purpose='task_params'
   AND b.bucket=p_bucket AND b.state='ready' AND b.v30_delivery_proof IS NOT NULL) THEN CONTINUE;END IF;
  -- Lock retained objects before deciding loss; another transaction cannot
  -- repair/revive a retained object while this transition is committed.
  PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id
   WHERE i.tenant_id=e.tenant_id AND i.task_id=t.id ORDER BY a.id FOR SHARE OF a;
  IF NOT EXISTS(SELECT FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
   ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
   WHERE i.tenant_id=e.tenant_id AND i.task_id=t.id AND
   (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
    OR a.object_version IS NULL OR a.object_version IN('','null')
    OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN CONTINUE;END IF;
  -- A pending producer is not evidence of permanent loss.
  IF EXISTS(SELECT FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
   JOIN photo.tasks producer ON producer.tenant_id=a.tenant_id AND producer.id=a.created_by_task_id
   WHERE i.tenant_id=e.tenant_id AND i.task_id=t.id AND producer.state NOT IN('succeeded','failed','cancelled','skipped')) THEN CONTINUE;END IF;
  SELECT package.* INTO d FROM photo.delivery_packages package JOIN photo.v2_workflows w
   ON w.tenant_id=package.tenant_id AND w.id=package.workflow_id
   WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id AND w.workflow_type='delivery'
   AND package.project_id=w.project_id AND package.snapshot_id=w.input_snapshot_id
   AND package.state='building' AND package.published_at IS NULL AND package.manifest_artifact_id IS NULL FOR UPDATE OF package;
  IF NOT FOUND THEN CONTINUE;END IF;
  PERFORM id FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id ORDER BY id FOR UPDATE;
  IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('leased','running'))
   OR EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id<>e.id AND state<>'closed') THEN CONTINUE;END IF;
  PERFORM id FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id ORDER BY id FOR UPDATE;
  IF r.revision>=9007199254740991 OR d.revision>=9007199254740991 OR EXISTS(
   SELECT FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id AND revision>=9007199254740991)
   THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
  PERFORM id FROM photo.runs WHERE tenant_id=e.tenant_id AND id<>e.run_id AND config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text ORDER BY id FOR UPDATE;
  IF NOT EXISTS(SELECT FROM photo.v30_delivery_runtime_leases WHERE instance_id=p_instance AND bucket=p_bucket AND expires_at>clock_timestamp()) THEN CONTINUE;END IF;
  UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=e.tenant_id AND config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text AND status NOT IN('completed','completed_with_errors','failed','cancelled');
  UPDATE photo.delivery_packages SET state='failed',revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=e.tenant_id AND id=d.id;
  UPDATE photo.delivery_items SET state='failed',reason='SOURCE_OUTPUT_UNAVAILABLE',revision=revision+1,updated_at=clock_timestamp()
   WHERE tenant_id=e.tenant_id AND package_id=d.id AND state NOT IN('ready','omitted','failed');
  UPDATE photo.tasks SET state='cancelled',cancel_requested=true WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('blocked','ready');
  UPDATE photo.graph_expansions SET state='closed' WHERE id=e.id;
  UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=e.run_id;
  INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(e.tenant_id,0) ON CONFLICT DO NOTHING;
  UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=e.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
  IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
  event_id:=photo.v30_uuid5(e.run_id,'delivery:source-unavailable:1');
  INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
   VALUES(e.tenant_id,next_cursor,e.run_id,r.created_by,'principal','v2.run.failed',jsonb_build_object('run_id',e.run_id,'status','failed','reason','SOURCE_OUTPUT_UNAVAILABLE'));
  INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(e.tenant_id,event_id,'photo.events.v1','v2.run.failed',
   jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.failed','tenant_id',e.tenant_id,'run_id',e.run_id,'status','failed','reason','SOURCE_OUTPUT_UNAVAILABLE'));
  RETURN true;
 END LOOP;
 RETURN false;
END $$;
ALTER FUNCTION photo.fail_next_v30_delivery_missing_source(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_next_v30_delivery_missing_source(uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_next_v30_delivery_missing_source(uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- Exact physical failures are reported only by the current bound scheduler.
-- Diagnostics contain immutable identity, never object keys or signed URLs.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_v30_delivery_physical_source(p_instance uuid,p_bucket text,p_expansion uuid,p_token uuid,p_artifact uuid,p_sha text,p_version text,p_code text,p_slot integer,p_request text,p_watermark boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint record;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;d photo.delivery_packages%ROWTYPE;t photo.tasks%ROWTYPE;
 page jsonb;entry jsonb;after_ordinal integer:=-1;pages integer:=0;matched boolean:=false;old_tenant text;diagnostic jsonb;event_id uuid;next_cursor bigint;
BEGIN
 IF p_instance IS NULL OR p_token IS NULL OR p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_version IS NULL OR p_version IN('','null')
 OR p_code IS NULL OR p_code NOT IN('SOURCE_OBJECT_MISSING','SOURCE_OBJECT_INTEGRITY','SOURCE_NPY_INVALID') OR p_watermark IS NULL
 OR p_slot IS NULL OR p_slot NOT BETWEEN 0 AND 79999 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152
 OR NOT EXISTS(SELECT FROM photo.v30_delivery_runtime_leases WHERE instance_id=p_instance AND bucket=p_bucket AND expires_at>clock_timestamp()) THEN RETURN false;END IF;
 SELECT tenant_id,run_id INTO hint FROM photo.graph_expansions WHERE id=p_expansion;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=hint.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=r.tenant_id AND run_id=r.id AND id=p_expansion FOR UPDATE;
 IF NOT FOUND OR e.kind<>'v2.delivery/1' OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=e.parent_task_id AND run_id=e.run_id FOR SHARE;
 IF NOT FOUND OR t.operation<>'v2.delivery.prepare' OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.cancel_requested
 OR NOT EXISTS(SELECT FROM photo.task_results z WHERE z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token)
 OR NOT EXISTS(SELECT FROM photo.inbox WHERE tenant_id=t.tenant_id AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN RETURN false;END IF;
 old_tenant:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',e.tenant_id::text,true);
 IF p_request<>'{}' THEN
 LOOP
  pages:=pages+1;IF pages>1001 THEN EXIT;END IF;
  IF p_watermark THEN page:=photo.v30_watermark_child_sources(e.id,p_token,p_slot,p_request,after_ordinal);
  ELSE page:=photo.v30_delivery_child_sources(e.id,p_token,p_slot,p_request,after_ordinal);END IF;
  IF page IS NULL OR jsonb_typeof(page->'items') IS DISTINCT FROM 'array' THEN EXIT;END IF;
  FOR entry IN SELECT value FROM jsonb_array_elements(page->'items') LOOP
   IF entry->'ref'->>'artifact_id'=p_artifact::text AND entry->'ref'->>'sha256'=p_sha AND entry->>'object_version'=p_version AND entry->>'bucket'=p_bucket THEN matched:=true;EXIT;END IF;
  END LOOP;
  IF matched OR page->'next' IS NULL OR page->'next'='null'::jsonb THEN EXIT;END IF;
  IF (page->>'next')::integer<=after_ordinal THEN EXIT;END IF;after_ordinal:=(page->>'next')::integer;
 END LOOP;
 ELSE
  -- Derivative verification precedes the v30-stage-verified inbox marker.
  -- The original prepare adoption remains mandatory; the exact canonical
  -- child result and fenced output upload are independently repeated here.
  IF photo.v30_delivery_expansion_ready(e.id) IS TRUE THEN
   -- Reuse the actual child-stage authority: Delivery children deliberately
   -- have no standalone v2_workflows row, so v2_adoption_artifact is not a
   -- valid authority for them. These accessors repeat frozen slot/config,
   -- deterministic producer, all canonical output uploads and current fences.
   FOR entry IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
    photo.v30_delivery_stage_result(e.id,p_token,p_slot,'derivative.resize'),
    photo.v30_delivery_stage_result(e.id,p_token,p_slot,'derivative.sharpen'),
    photo.v30_watermark_stage_result(e.id,p_token,p_slot,'derivative.watermark'))) LOOP
    IF entry IS NOT NULL AND entry<>'null'::jsonb AND EXISTS(
     SELECT FROM jsonb_array_elements(entry->'outputs') output
     WHERE output->'ref'->>'artifact_id'=p_artifact::text
      AND output->'ref'->>'sha256'=p_sha AND output->>'object_version'=p_version
      AND output->>'bucket'=p_bucket) THEN matched:=true;EXIT;END IF;
   END LOOP;
  END IF;
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(old_tenant,''),true);
 IF NOT matched THEN RETURN false;END IF;
 -- Recheck exact retained version after independently scoped source discovery.
 PERFORM id FROM photo.artifacts WHERE tenant_id=e.tenant_id AND id=p_artifact AND sha256=p_sha AND object_version=p_version AND state='ready' AND verified_at IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT package.* INTO d FROM photo.delivery_packages package JOIN photo.v2_workflows w ON w.tenant_id=package.tenant_id AND w.id=package.workflow_id
 WHERE w.tenant_id=e.tenant_id AND w.run_id=e.run_id AND w.workflow_type='delivery' AND package.project_id=w.project_id AND package.snapshot_id=w.input_snapshot_id
 AND package.state='building' AND package.published_at IS NULL AND package.manifest_artifact_id IS NULL FOR UPDATE OF package;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id ORDER BY id FOR UPDATE;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('leased','running'))
 OR EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id<>e.id AND state<>'closed') THEN RETURN false;END IF;
 PERFORM id FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id ORDER BY id FOR UPDATE;
 IF r.revision>=9007199254740991 OR d.revision>=9007199254740991 OR EXISTS(SELECT FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id AND revision>=9007199254740991) THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 -- Already admitted child graphs retain their own cancellation/GC settlement.
 -- No immutable source or successful output is deleted by this transition.
 PERFORM id FROM photo.runs WHERE tenant_id=e.tenant_id AND id<>e.run_id AND config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text ORDER BY id FOR UPDATE;
 -- Source discovery can consume time while row locks prevent renewal.
 -- Repeat both live clocks immediately before any terminal mutation.
 IF e.expansion_lease_until<=clock_timestamp() OR NOT EXISTS(SELECT FROM photo.v30_delivery_runtime_leases WHERE instance_id=p_instance AND bucket=p_bucket AND expires_at>clock_timestamp()) THEN RETURN false;END IF;
 UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=e.tenant_id AND config_snapshot->'v30_delivery_export'->>'parent_run_id'=e.run_id::text AND status NOT IN('completed','completed_with_errors','failed','cancelled');
 UPDATE photo.delivery_packages SET state='failed',revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=e.tenant_id AND id=d.id;
 UPDATE photo.delivery_items SET state='failed',reason=p_code,revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=e.tenant_id AND package_id=d.id AND state NOT IN('ready','omitted','failed');
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND state IN('blocked','ready');
 UPDATE photo.graph_expansions SET state='closed' WHERE id=e.id;
 UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=e.run_id;
 diagnostic:=jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha,'object_version',p_version,'reason',p_code,'slot_index',p_slot);
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(e.tenant_id,0) ON CONFLICT DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=e.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(e.run_id,'delivery:physical-source-failed:1');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload) VALUES(e.tenant_id,next_cursor,e.run_id,r.created_by,'principal','v2.run.failed',jsonb_build_object('run_id',e.run_id,'status','failed','reason',p_code,'source_failure',diagnostic));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(e.tenant_id,event_id,'photo.events.v1','v2.run.failed',jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.failed','tenant_id',e.tenant_id,'run_id',e.run_id,'status','failed','reason',p_code,'source_failure',diagnostic));
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
-- Physical SHA validation does not prove a replacement locator/version was
-- the accepted upload. Repeat its complete descriptor at stage discovery.
DO $$
DECLARE signature text;definition text;needle text;replacement text;
BEGIN
 needle:='AND a.sha256=u.sha256 AND a.output_role=u.role';
 replacement:=needle||' AND a.bucket=u.bucket AND a.object_key=u.object_key'
  ||' AND a.object_version=u.object_version AND a.bytes=u.bytes AND a.media_type=u.media_type';
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_delivery_stage_result(uuid,uuid,integer,text)',
  'photo.v30_watermark_stage_result(uuid,uuid,integer,text)'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'DELIVERY_STAGE_UPLOAD_DESCRIPTOR_DEFINITION_DRIFT';END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
