-- +goose Up
-- Recompute the canonical frozen inventory digest; JSONB equality alone does
-- not establish the exact sorted entry set used by the Go task compiler.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_inventory_digest_ready(p_task uuid,p_params uuid,
 p_params_sha text,p_operation text,p_input_digest text,p_entries jsonb,p_body jsonb,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb;ref jsonb;aid uuid;prior uuid;contract_name text;
 body text:='[';first_item boolean:=true;params_count integer:=0;array_count integer:=0;
 required_contract text;required_ref jsonb;source jsonb;target jsonb;guard jsonb;donor jsonb;
 donor_mode boolean;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid
 OR p_params IS NULL OR p_params='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_input_digest,'') !~ '^[0-9a-f]{64}$'
 OR p_operation NOT IN ('portrait.glare','image.reflection')
 OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_entries) NOT BETWEEN 6 AND 127
 OR jsonb_typeof(p_body) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 source:=p_body->'base'->'source';guard:=p_body->'protected';
 target:=CASE p_operation WHEN 'portrait.glare' THEN p_body->'glasses_mask' ELSE p_body->'mask' END;
 donor_mode:=p_operation='portrait.glare' AND p_body->>'mode'='donor';
 donor:=p_body->'donor'->'source';
 IF (p_operation='image.reflection' AND
  ((p_body ? 'mode') OR (p_body ? 'donor') OR (p_body ? 'donor_faces')))
 OR (p_operation='portrait.glare' AND
  ((coalesce(p_body->>'mode','') NOT IN('donor','generative')) OR
   (donor_mode AND ((p_body ? 'donor') IS DISTINCT FROM true OR
    (p_body ? 'donor_faces') IS DISTINCT FROM true)) OR
   (NOT donor_mode AND ((p_body ? 'donor') OR (p_body ? 'donor_faces')))))
 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=item->'ref';contract_name:=item->>'contract';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR
   (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(item) k) IS DISTINCT FROM
    ARRAY['contract','ref']::text[]
   OR contract_name NOT IN('Params','WorkingImage','MaskRef','NpyArray','Report',
    'Calibration','DonorConfirmation','FaceObservations')
   OR photo.v25_layer_ref_shape(ref) IS DISTINCT FROM true THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF prior IS NOT NULL AND aid<=prior THEN RETURN false;END IF;
  IF aid=p_params THEN
   params_count:=params_count+1;
   IF contract_name<>'Params' OR ref->>'sha256'<>p_params_sha THEN RETURN false;END IF;
  END IF;
  IF contract_name='NpyArray' THEN array_count:=array_count+1;END IF;
  IF NOT first_item THEN body:=body||',';END IF;
  body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||
   '","sha256":"'||(ref->>'sha256')||'"}}';
  first_item:=false;prior:=aid;
 END LOOP;
 IF params_count<>1 OR array_count<(CASE WHEN donor_mode THEN 4 ELSE 3 END)
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='Params')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='Report')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='Calibration')<>1
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='MaskRef')<>2
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='WorkingImage')<>
  (CASE WHEN donor_mode THEN 2 ELSE 1 END)
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='FaceObservations')<>
  (CASE WHEN p_operation='portrait.glare' THEN CASE WHEN donor_mode THEN 2 ELSE 1 END ELSE 0 END)
 OR (SELECT count(*) FROM jsonb_array_elements(p_entries) e WHERE e->>'contract'='DonorConfirmation')<>
  (CASE WHEN p_operation='portrait.glare' THEN 1 ELSE 0 END)
 THEN RETURN false;END IF;
 FOR required_contract,required_ref IN
  SELECT * FROM (VALUES('WorkingImage'::text,source),('MaskRef'::text,target),
   ('MaskRef'::text,guard),('Report'::text,p_pin->'report'),
   ('Calibration'::text,p_pin->'calibration'),
   ('Params'::text,jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)),
   ('WorkingImage'::text,CASE WHEN donor_mode THEN donor ELSE NULL END),
   ('DonorConfirmation'::text,CASE WHEN p_operation='portrait.glare' THEN p_body->'confirmation' ELSE NULL END),
   ('FaceObservations'::text,CASE WHEN p_operation='portrait.glare' THEN p_body->'source_faces' ELSE NULL END),
   ('FaceObservations'::text,CASE WHEN donor_mode THEN p_body->'donor_faces' ELSE NULL END)) required(contract,ref)
  WHERE ref IS NOT NULL LOOP
  IF photo.v25_layer_ref_shape(required_ref) IS DISTINCT FROM true OR
   NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_entries) e
    WHERE e->>'contract'=required_contract AND e->'ref'=required_ref) THEN RETURN false;END IF;
 END LOOP;
 body:=body||']';
 RETURN encode(sha256(convert_to('{"entries":'||body||',"operation":"'||p_operation||
  '","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_inventory_digest_ready(uuid,uuid,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_inventory_digest_ready(uuid,uuid,text,text,text,jsonb,jsonb,jsonb) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.stage_v28_restoration_task(p_tenant uuid,p_run uuid,p_actor uuid,
 p_session uuid,p_descriptor text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;task jsonb;old photo.tasks%ROWTYPE;workflow_id uuid;task_id uuid;h bytea;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;task:=plan->'task';
 IF jsonb_typeof(task) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(task) k) IS DISTINCT FROM
  ARRAY['execution_profile','id','input_digest','node_key','operation',
  'operation_version','params_artifact_id','params_sha','resource_class']::text[]
 OR (task->>'node_key' ~ '^(portrait[.]glare|image[.]reflection):(0|[1-9][0-9]{0,3})$') IS DISTINCT FROM true
 OR split_part(task->>'node_key',':',2)::integer>=5000
 OR split_part(task->>'node_key',':',1) IS DISTINCT FROM task->>'operation'
 OR photo.v28_restoration_inventory_digest_ready((task->>'id')::uuid,
  (task->>'params_artifact_id')::uuid,task->>'params_sha',task->>'operation',
  task->>'input_digest',plan->'entries',plan->'params',
  task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor
  AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT id INTO workflow_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run
  AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 h:=public.digest(uuid_send(workflow_id)||convert_to(task->>'node_key','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 task_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF task_id::text IS DISTINCT FROM task->>'id' THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id FOR UPDATE;
 IF FOUND AND (old.run_id IS DISTINCT FROM p_run OR old.node_key IS DISTINCT FROM task->>'node_key'
 OR old.operation IS DISTINCT FROM task->>'operation' OR old.operation_version IS DISTINCT FROM '1'
 OR old.resource_class IS DISTINCT FROM 'gpu' OR old.params_artifact_id::text IS DISTINCT FROM task->>'params_artifact_id'
 OR old.input_digest IS DISTINCT FROM task->>'input_digest' OR old.state IS DISTINCT FROM 'blocked'
 OR old.attempt IS DISTINCT FROM 0 OR old.cancel_requested IS DISTINCT FROM false
 OR (old.execution_profile IS DISTINCT FROM task->'execution_profile' AND NOT(photo.v2_input_closure_ready(old.id)
  AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=
   task->'execution_profile'))) THEN RETURN false;END IF;
 IF photo.save_v28_restoration_descriptor(p_tenant,p_run,p_actor,p_session,
  p_descriptor,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF old.id IS NOT NULL THEN RETURN true;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,
  resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_run,task->>'node_key',task->>'operation','1','gpu',
  'blocked','[]'::jsonb,(task->>'params_artifact_id')::uuid,task->>'input_digest',
  task->'execution_profile');
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v28_restoration_task(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v28_restoration_task(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v28_restoration_task(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
