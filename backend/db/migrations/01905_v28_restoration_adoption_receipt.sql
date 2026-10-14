-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v28_restoration_adoption_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v28_restoration_adoption_receipt_shape CHECK (
 v28_restoration_adoption_receipt IS NULL OR
 (v28_restoration_descriptor_jcs IS NOT NULL AND
  jsonb_typeof(v28_restoration_adoption_receipt)='object' AND
  pg_column_size(v28_restoration_adoption_receipt)<=65536));

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v28_restoration_adoption_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v28_restoration_adoption_receipt IS NOT NULL THEN
   RAISE EXCEPTION 'V28 receipt requires verified commit' USING ERRCODE='42501';END IF;
 ELSIF NEW.v28_restoration_adoption_receipt IS DISTINCT FROM OLD.v28_restoration_adoption_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v28_restoration_adoption_receipt IS NOT NULL
   OR OLD.v28_restoration_descriptor_jcs IS NULL OR NOT OLD.graph_sealed
   OR OLD.cancel_requested THEN
   RAISE EXCEPTION 'V28 receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v28_restoration_adoption_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v28_restoration_adoption_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v28_restoration_adoption_receipt_guard
 BEFORE INSERT OR UPDATE ON photo.runs FOR EACH ROW
 EXECUTE FUNCTION photo.guard_v28_restoration_adoption_receipt();

CREATE FUNCTION photo.adopt_v28_restoration_verified(p_tenant uuid,p_task uuid,
 p_result uuid,p_evidence text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 project_id uuid;item jsonb;member photo.artifacts%ROWTYPE;kind text;role text;
 key text;seen text[]:='{}';expected text[];candidate jsonb;ref jsonb;
 root uuid;root_sha text;count_value bigint;expected_role text;receipt jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_evidence IS NULL OR octet_length(p_evidence) NOT BETWEEN 2 AND 65536
 THEN RETURN false;END IF;
 e:=p_evidence::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object' OR
  (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(e) k) IS DISTINCT FROM
   ARRAY['bucket','candidate','changed_pixels','objects','operation','scope']::text[]
  OR length(e->>'bucket') NOT BETWEEN 3 AND 63
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(e->'scope') k) IS DISTINCT FROM
   ARRAY['result','root','root_sha','task','tenant']::text[]
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(e->'candidate') k) IS DISTINCT FROM
   ARRAY['changed','changed_pixels','generated','generated_pixels','image',
    'pixels','report','result']::text[]
  OR e->'scope'->>'tenant' IS DISTINCT FROM p_tenant::text
  OR e->'scope'->>'task' IS DISTINCT FROM p_task::text
  OR e->'scope'->>'result' IS DISTINCT FROM p_result::text
  OR e->>'operation' NOT IN('portrait.glare','image.reflection')
  OR jsonb_typeof(e->'objects') IS DISTINCT FROM 'array'
  OR jsonb_array_length(e->'objects') NOT IN(10,12)
  OR (e->>'changed_pixels')!~'^(0|[1-9][0-9]{0,9})$'
 THEN RETURN false;END IF;
 root:=(e->'scope'->>'root')::uuid;root_sha:=e->'scope'->>'root_sha';
 count_value:=(e->>'changed_pixels')::bigint;candidate:=e->'candidate';
 IF root_sha!~'^[0-9a-f]{64}$' OR candidate->'result' IS DISTINCT FROM
  jsonb_build_object('artifact_id',root,'sha256',root_sha)
  OR photo.v25_layer_ref_shape(candidate->'image') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(candidate->'report') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(candidate->'changed') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(candidate->'pixels') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(candidate->'changed_pixels') IS DISTINCT FROM true
  OR (candidate->'generated' IS DISTINCT FROM 'null'::jsonb) IS DISTINCT FROM
   (candidate->'generated_pixels' IS DISTINCT FROM 'null'::jsonb)
  OR (candidate->'generated' IS DISTINCT FROM 'null'::jsonb) IS DISTINCT FROM
   (jsonb_array_length(e->'objects')=12)
 THEN RETURN false;END IF;
 expected:=ARRAY['array/changed_pixels','array/output_pixels','array/protected_pixels',
  'array/source_pixels','array/target_pixels','document/changed','document/image',
  'document/report','document/result','document/source'];
 IF jsonb_array_length(e->'objects')=12 THEN
  expected:=expected||ARRAY['array/generated_pixels','document/generated'];
  IF photo.v25_layer_ref_shape(candidate->'generated') IS DISTINCT FROM true OR
   photo.v25_layer_ref_shape(candidate->'generated_pixels') IS DISTINCT FROM true
  THEN RETURN false;END IF;
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
  AND task_id=p_task FOR SHARE;
 IF r.id IS NULL OR t.id IS NULL OR z.id IS NULL OR r.kind<>'v2'
 OR r.status NOT IN('running','waiting_review') OR NOT r.graph_sealed
 OR r.cancel_requested OR r.v28_restoration_descriptor_jcs IS NULL
 OR t.operation IS DISTINCT FROM e->>'operation' OR t.operation_version<>'1'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.canonical_result_id IS DISTINCT FROM p_result
 OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token
 OR NOT z.review_required OR z.outputs IS DISTINCT FROM jsonb_build_array(
  jsonb_build_object('artifact_id',root,'sha256',root_sha))
 OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
  AND i.event_id=photo.v2_adoption_event_id(p_task,p_result))
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true
 OR (t.operation='image.reflection' AND jsonb_array_length(e->'objects')<>12)
 OR (t.operation='portrait.glare' AND
   (r.v28_restoration_descriptor_jcs::jsonb)->'params'->>'mode'='generative'
   AND jsonb_array_length(e->'objects')<>12)
 OR (t.operation='portrait.glare' AND
   (r.v28_restoration_descriptor_jcs::jsonb)->'params'->>'mode'='donor'
   AND jsonb_array_length(e->'objects')<>10)
 THEN RETURN false;END IF;
 SELECT project_id INTO project_id FROM photo.v2_workflows WHERE tenant_id=p_tenant
  AND run_id=r.id AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF project_id IS NULL OR photo.lock_v28_restoration_qualification(p_tenant,project_id,
  r.created_by,r.v28_restoration_session_id,t.operation,
  t.execution_profile->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(e->'objects') LOOP
  kind:=item->>'kind';role:=item->>'role';key:=kind||'/'||role;ref:=item->'ref';
  IF key<>ALL(expected) OR key=ANY(seen)
   OR photo.v25_layer_ref_shape(ref) IS DISTINCT FROM true
   OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(item) k) IS DISTINCT FROM
    ARRAY['bytes','key','kind','media_type','ref','role','version']::text[]
   OR item->>'version' IN('','null') OR item->>'key' IS NULL
   OR item->'bytes' IS NULL OR item->>'media_type' IS NULL
  THEN RETURN false;END IF;
  seen:=array_append(seen,key);
  IF key='document/result' AND ref IS DISTINCT FROM candidate->'result'
   OR key='document/source' AND ref IS DISTINCT FROM
    (r.v28_restoration_descriptor_jcs::jsonb)->'params'->'base'->'source'
   OR key='array/source_pixels' AND ref IS DISTINCT FROM
    (SELECT x.value->'ref' FROM jsonb_array_elements(
      (r.v28_restoration_descriptor_jcs::jsonb)->'entries') x
     WHERE x.value->>'contract'='NpyArray' AND x.value->'ref'=ref LIMIT 1)
   OR key='array/target_pixels' AND ref IS DISTINCT FROM
    (r.v28_restoration_descriptor_jcs::jsonb)->'origin'->'target_pixel'
   OR key='array/protected_pixels' AND ref IS DISTINCT FROM
    (r.v28_restoration_descriptor_jcs::jsonb)->'origin'->'protected_pixel'
   OR key='document/image' AND ref IS DISTINCT FROM candidate->'image'
   OR key='document/report' AND ref IS DISTINCT FROM candidate->'report'
   OR key='document/changed' AND ref IS DISTINCT FROM candidate->'changed'
   OR key='document/generated' AND ref IS DISTINCT FROM candidate->'generated'
   OR key='array/output_pixels' AND ref IS DISTINCT FROM candidate->'pixels'
   OR key='array/changed_pixels' AND ref IS DISTINCT FROM candidate->'changed_pixels'
   OR key='array/generated_pixels' AND ref IS DISTINCT FROM candidate->'generated_pixels'
  THEN RETURN false;END IF;
  SELECT * INTO member FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
   AND a.bucket=e->>'bucket' AND a.object_key=item->>'key'
   AND a.object_version=item->>'version'
   AND a.bytes=(item->>'bytes')::bigint AND a.media_type=item->>'media_type'
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  expected_role:=CASE key WHEN 'document/result' THEN 'document'
   WHEN 'document/image' THEN 'working_image' WHEN 'document/report' THEN 'report'
   WHEN 'document/changed' THEN 'mask' WHEN 'document/generated' THEN 'mask'
   WHEN 'array/output_pixels' THEN 'pixels'
   WHEN 'array/changed_pixels' THEN 'mask_pixels'
   WHEN 'array/generated_pixels' THEN 'mask_pixels' ELSE NULL END;
  IF kind='document' AND member.media_type<>'application/json'
   OR kind='array' AND member.media_type<>'application/x-npy' THEN RETURN false;END IF;
  IF expected_role IS NOT NULL THEN
   IF member.output_role IS DISTINCT FROM expected_role
    OR member.created_by_task_id IS DISTINCT FROM p_task
    OR member.produced_attempt IS DISTINCT FROM t.attempt
    OR member.produced_fence IS DISTINCT FROM t.fencing_token THEN RETURN false;END IF;
  ELSIF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=member.id AND i.sha256=member.sha256
   AND i.contract=CASE kind WHEN 'document' THEN 'WorkingImage' ELSE 'NpyArray' END)
  THEN RETURN false;END IF;
 END LOOP;
 IF (SELECT array_agg(x ORDER BY x) FROM unnest(seen) x) IS DISTINCT FROM
  (SELECT array_agg(x ORDER BY x) FROM unnest(expected) x) THEN RETURN false;END IF;
 receipt:=jsonb_build_object('schema_version',1,'task',p_task,'result',p_result,
  'attempt',t.attempt,'fence',t.fencing_token,'evidence_sha',
  encode(sha256(convert_to(p_evidence,'UTF8')),'hex'),'evidence',e);
 IF r.v28_restoration_adoption_receipt IS NOT NULL THEN
  RETURN r.v28_restoration_adoption_receipt IS NOT DISTINCT FROM receipt;END IF;
 UPDATE photo.runs SET v28_restoration_adoption_receipt=receipt
  WHERE tenant_id=p_tenant AND id=r.id;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
