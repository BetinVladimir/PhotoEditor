-- +goose Up
-- Serialize tenant before parent/child locks and reuse normal attempt cancellation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.begin_v26_render_batch(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
 p_batch uuid,p_manifest_jcs text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb; part jsonb; old photo.runs%ROWTYPE; digest text; h bytea; expected uuid;
 ordinal integer:=0; index integer:=0; total numeric:=0;
BEGIN
 IF p_batch IS NULL OR p_batch='00000000-0000-0000-0000-000000000000'::uuid
  OR p_manifest_jcs IS NULL OR octet_length(p_manifest_jcs) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 body:=p_manifest_jcs::jsonb;digest:=encode(sha256(convert_to(p_manifest_jcs,'UTF8')),'hex');
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body-'policy_sha'-'schema_version'-'project_id'-'input_snapshot_id'-'auth_session_id'-'parts'<>'{}'::jsonb
  OR coalesce(body->>'policy_sha','') !~ '^[0-9a-f]{64}$'
  OR body->'schema_version' IS DISTINCT FROM '1'::jsonb OR body->>'project_id' IS DISTINCT FROM p_project::text
  OR body->>'input_snapshot_id' IS DISTINCT FROM p_snapshot::text OR body->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR jsonb_typeof(body->'parts') IS DISTINCT FROM 'array' OR jsonb_array_length(body->'parts') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session) THEN RETURN false;END IF;
 FOR part IN SELECT value FROM jsonb_array_elements(body->'parts') LOOP
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' OR part-'run_id'-'first_ordinal'-'frame_count'-'config_sha'-'output_bytes'<>'{}'::jsonb
   OR coalesce(part->>'config_sha','') !~ '^[0-9a-f]{64}$'
   OR jsonb_typeof(part->'first_ordinal') IS DISTINCT FROM 'number' OR coalesce(part->>'first_ordinal','') !~ '^[0-9]+$'
   OR (part->>'first_ordinal')::numeric<>ordinal
   OR jsonb_typeof(part->'frame_count') IS DISTINCT FROM 'number' OR coalesce(part->>'frame_count','') !~ '^[1-9][0-9]*$'
   OR (part->>'frame_count')::numeric NOT BETWEEN 1 AND 5000
   OR jsonb_typeof(part->'output_bytes') IS DISTINCT FROM 'number' OR coalesce(part->>'output_bytes','') !~ '^[1-9][0-9]*$'
   OR (part->>'output_bytes')::numeric>9007199254740991 THEN RETURN false;END IF;
  ordinal:=ordinal+(part->>'frame_count')::integer;total:=total+(part->>'output_bytes')::numeric;
  IF ordinal>5000 OR total>9007199254740991 THEN RETURN false;END IF;
  h:=public.digest(uuid_send(p_batch)||convert_to('render.batch:'||index::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF part->>'run_id' IS DISTINCT FROM expected::text THEN RETURN false;END IF;
  index:=index+1;
 END LOOP;
 IF ordinal IS DISTINCT FROM (SELECT s.item_count FROM photo.snapshots s WHERE s.tenant_id=p_tenant AND s.id=p_snapshot) THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 -- Serialize creation/replay even before the parent row exists.
 PERFORM pg_advisory_xact_lock(hashtextextended('v26-render-batch:'||p_batch::text,0));
 SELECT * INTO old FROM photo.runs WHERE id=p_batch AND tenant_id=p_tenant FOR UPDATE;
 IF FOUND THEN RETURN old.kind='bulk' AND old.status='draft' AND NOT old.cancel_requested AND NOT old.graph_sealed
  AND old.created_by=p_actor AND old.input_snapshot_id=p_snapshot AND old.config_hash=digest AND old.config_snapshot=body;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(p_batch,p_tenant,'bulk','draft',p_snapshot,body,digest,p_actor,'fail_fast',false);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR unique_violation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v26_render_batch(uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v26_render_batch(uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v26_render_batch(uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_cancellation_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT NOT EXISTS(SELECT 1 FROM photo.runs parent
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(parent.config_snapshot->'parts')='array'
   THEN parent.config_snapshot->'parts' ELSE '[]'::jsonb END) part
  JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id::text=part->>'run_id'
   AND child.kind='v2' AND child.created_by=parent.created_by AND child.input_snapshot_id=parent.input_snapshot_id
   AND child.config_hash=part->>'config_sha'
   AND child.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
   AND child.config_snapshot->'v26_render'->>'auth_session_id'=parent.config_snapshot->>'auth_session_id'
  WHERE parent.tenant_id=p_tenant AND parent.id=p_run AND parent.kind='bulk'
   AND parent.config_snapshot->'schema_version'='1'::jsonb
   AND parent.config_snapshot ?& ARRAY['parts','policy_sha','auth_session_id']
   AND child.status NOT IN('completed','completed_with_errors','failed','cancelled'));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_cancellation_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_cancellation_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_v26_render_batch(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; child photo.runs%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.kind<>'bulk' OR parent.created_by IS DISTINCT FROM p_actor
  OR parent.config_snapshot->>'auth_session_id' IS DISTINCT FROM p_session::text
  OR parent.config_snapshot->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR jsonb_typeof(parent.config_snapshot->'parts') IS DISTINCT FROM 'array'
  OR NOT parent.config_snapshot ? 'policy_sha'
  OR NOT photo.v2_editor_authorized(p_tenant,(parent.config_snapshot->>'project_id')::uuid,p_actor,p_session) THEN RETURN false;END IF;
 IF parent.status NOT IN('draft','queued','running','waiting_review','cancelling','cancelled') THEN RETURN false;END IF;
 FOR child IN SELECT c.* FROM photo.runs c JOIN jsonb_array_elements(parent.config_snapshot->'parts') part
  ON c.id::text=part->>'run_id' AND c.config_hash=part->>'config_sha'
  WHERE c.tenant_id=p_tenant AND c.kind='v2' AND c.created_by=p_actor AND c.input_snapshot_id=parent.input_snapshot_id
   AND c.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
   AND c.config_snapshot->'v26_render'->>'auth_session_id'=p_session::text
   AND c.status IN('draft','queued','running','waiting_review') ORDER BY c.id FOR UPDATE OF c LOOP
  IF child.revision>=9007199254740991 THEN RAISE EXCEPTION 'V26_CANCEL_REVISION_EXHAUSTED' USING ERRCODE='P2691';END IF;
  UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE id=child.id AND tenant_id=p_tenant;
 END LOOP;
 IF parent.status<>'cancelled' AND parent.status<>'cancelling' THEN
  IF parent.revision>=9007199254740991 THEN RAISE EXCEPTION 'V26_CANCEL_REVISION_EXHAUSTED' USING ERRCODE='P2691';END IF;
  UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE id=p_batch AND tenant_id=p_tenant;
  INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
  VALUES(p_tenant,'user',p_actor,'run.cancel_requested','run',p_batch,gen_random_uuid(),jsonb_build_object('render_batch',true));
 END IF;
 RETURN true;
EXCEPTION WHEN SQLSTATE 'P2691' OR invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_v26_render_batch(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_v26_render_batch(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_v26_render_batch(uuid,uuid,uuid,uuid) TO photo_scheduler,photo_api;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finalize_run_cancellation() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; r photo.runs%ROWTYPE; next_cursor bigint;
BEGIN
 SELECT rr.id INTO candidate FROM photo.runs rr
 WHERE rr.cancel_requested AND rr.status='cancelling'
 AND photo.v26_render_batch_cancellation_ready(rr.tenant_id,rr.id)
 AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=rr.tenant_id AND t.run_id=rr.id
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped'))
 ORDER BY rr.created_at,rr.id LIMIT 1;
 IF candidate IS NULL THEN RETURN 0;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=candidate FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT r.cancel_requested OR r.status<>'cancelling' THEN RETURN 0;END IF;
 IF NOT photo.v26_render_batch_cancellation_ready(r.tenant_id,r.id) THEN RETURN 0;END IF;
 IF EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN 0;END IF;
 IF r.kind='ingest' AND EXISTS(
  SELECT FROM photo.ingest_upload_bindings b
  LEFT JOIN photo.ingest_catalog_items c ON c.tenant_id=b.tenant_id AND c.run_id=b.run_id AND c.ordinal=b.ordinal AND c.settled_at IS NOT NULL
  LEFT JOIN photo.ingest_rejections f ON f.tenant_id=b.tenant_id AND f.run_id=b.run_id AND f.ordinal=b.ordinal
  WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND c.ordinal IS NULL AND f.ordinal IS NULL
 ) THEN RETURN 0;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;

 UPDATE photo.reviews SET state='cancelled',revision=revision+1
 WHERE tenant_id=r.tenant_id AND run_id=r.id AND state='pending' AND revision<9007199254740991;
 IF EXISTS(SELECT FROM photo.reviews WHERE tenant_id=r.tenant_id AND run_id=r.id AND state='pending') THEN
  RAISE EXCEPTION 'REVIEW_REVISION_EXHAUSTED' USING ERRCODE='23514';
 END IF;
 UPDATE photo.graph_expansions SET state='closed',expansion_token=NULL,expansion_lease_until=NULL
 WHERE tenant_id=r.tenant_id AND run_id=r.id AND state<>'closed';

 IF r.kind='ingest' THEN
  INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count)
  SELECT r.tenant_id,r.id,'complete',s.item_count FROM photo.snapshots s
  WHERE s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
  ON CONFLICT(run_id) DO UPDATE SET phase='complete';
  IF NOT FOUND THEN RAISE EXCEPTION 'INGEST_SNAPSHOT_MISSING' USING ERRCODE='23514';END IF;
 END IF;

 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0)
 ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
 WHERE tenant_id=r.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='cancelled',graph_sealed=true,revision=revision+1
 WHERE tenant_id=r.tenant_id AND id=r.id;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(r.tenant_id,next_cursor,r.id,r.created_by,'principal','run.cancelled',
  jsonb_build_object('run_id',r.id,'status','cancelled','revision',r.revision+1));
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_run_cancellation() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_run_cancellation() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_run_cancellation() TO photo_scheduler,photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
