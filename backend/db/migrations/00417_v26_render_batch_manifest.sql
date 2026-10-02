-- +goose Up
-- Persist compact batch identity using existing tenant-scoped runs; child plans
-- retain their complete evidence in their own immutable V26 run configs.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v26_render_batch(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,
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
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
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
CREATE FUNCTION photo.v26_render_batch_manifest(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(manifest jsonb,manifest_sha text,cancel_requested boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.config_snapshot,r.config_hash,r.cancel_requested FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE r.tenant_id=p_tenant AND r.id=p_batch AND r.kind='bulk' AND r.created_by=p_actor
  AND r.config_snapshot->'schema_version'='1'::jsonb AND jsonb_typeof(r.config_snapshot->'parts')='array'
  AND r.config_snapshot->>'auth_session_id'=p_session::text
  AND photo.v2_editor_authorized(p_tenant,(r.config_snapshot->>'project_id')::uuid,p_actor,p_session);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_manifest(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_manifest(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_batch_manifest(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_parts(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(part_ordinal bigint,run_id uuid,first_ordinal integer,frame_count integer,run_status text,
 task_count bigint,succeeded_count bigint,failed_count bigint,cancelled_count bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT part.ordinality-1,(part.value->>'run_id')::uuid,(part.value->>'first_ordinal')::integer,
  (part.value->>'frame_count')::integer,coalesce(child.status,'missing'),count(t.id),
  count(t.id) FILTER(WHERE t.state='succeeded'),count(t.id) FILTER(WHERE t.state='failed'),
  count(t.id) FILTER(WHERE t.state='cancelled')
 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session) parent
 CROSS JOIN LATERAL jsonb_array_elements(parent.manifest->'parts') WITH ORDINALITY part
 LEFT JOIN photo.runs child ON child.tenant_id=p_tenant AND child.id=(part.value->>'run_id')::uuid
  AND child.kind='v2' AND child.created_by=p_actor AND child.config_hash=part.value->>'config_sha'
  AND child.input_snapshot_id=(parent.manifest->>'input_snapshot_id')::uuid
  AND child.config_snapshot->>'project_id'=parent.manifest->>'project_id'
  AND child.config_snapshot->'v26_render'->>'auth_session_id'=p_session::text
 LEFT JOIN photo.tasks t ON t.tenant_id=child.tenant_id AND t.run_id=child.id
  AND t.operation='render.compose' AND t.operation_version='1'
 GROUP BY part.ordinality,part.value,child.status ORDER BY part.ordinality;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_parts(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_parts(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_batch_parts(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
