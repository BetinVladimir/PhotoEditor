-- +goose Up
-- Register blocked tasks only. A separate seal must authorize dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v26_render_derivative_task(p_tenant uuid,p_run uuid,p_variant text,p_root text,p_image text,p_task jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;image jsonb;inputs jsonb;profile jsonb;
 aid uuid;task_id uuid;pixels bigint;target bigint;edge bigint;cost bigint;scratch bigint;
 refs_text text;digest text;ref jsonb;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object'
 OR NOT p_task ?& ARRAY['id','node_key','operation','operation_version','resource_class','timeout_seconds','params','inputs','input_digest','execution_profile']
 OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR p_variant NOT IN('preview1600','preview2560') THEN RETURN false;END IF;
 -- Same lock order as pin/completion, before reading the durable document.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 task_id:=(p_task->>'id')::uuid;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=task_id AND purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.state<>'ready' OR d.v26_derivative_params IS NULL THEN RETURN false;END IF;
 aid:=photo.pin_v26_render_derivative_document(p_tenant,p_run,p_variant,p_root,p_image,d.v26_derivative_params,d.bucket);
 IF aid IS DISTINCT FROM d.artifact_id THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=aid AND a.sha256=d.sha256
 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||aid
 AND a.state='ready' AND a.output_role='task_params'
 AND a.media_type='application/json' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 image:=p_image::jsonb;
 inputs:=jsonb_build_array((p_root::jsonb)->'working_image',image->'pixels');
 IF image->'alpha'<>'null'::jsonb THEN inputs:=inputs||jsonb_build_array(image->'alpha');END IF;
 IF (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(inputs))<>jsonb_array_length(inputs)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) WHERE value->>'artifact_id'=aid::text) THEN RETURN false;END IF;
 IF p_task->'inputs' IS DISTINCT FROM inputs OR p_task->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',aid,'sha256',d.sha256)
 OR p_task->>'node_key' IS DISTINCT FROM 'render.derivative:'||p_variant
 OR p_task->>'operation' IS DISTINCT FROM 'media.preview' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'media' OR p_task->'timeout_seconds' IS DISTINCT FROM '300'::jsonb
 OR NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation='media.preview' AND version='1' AND resource_class='media' AND timeout_seconds=300)
 THEN RETURN false;END IF;
 IF (image->'canvas'->>'width')::bigint NOT BETWEEN 1 AND 200000 OR (image->'canvas'->>'height')::bigint NOT BETWEEN 1 AND 200000 THEN RETURN false;END IF;
 pixels:=(image->'canvas'->>'width')::bigint*(image->'canvas'->>'height')::bigint;
 IF pixels IS NULL OR pixels NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 edge:=CASE p_variant WHEN 'preview1600' THEN 1600 ELSE 2560 END;target:=least(pixels,edge*edge);
 cost:=CASE WHEN image->'alpha'='null'::jsonb THEN 24 ELSE 96 END;
 scratch:=67108864+pixels*CASE WHEN image->'alpha'='null'::jsonb THEN 12 ELSE 16 END;
 profile:=jsonb_build_object('backend','cpu','precision','float32','resource_estimate',jsonb_build_object(
 'ram_bytes',pixels*cost+target*96+134217728,'vram_bytes',0,'scratch_bytes',scratch,'output_bytes',54525952,'pixel_count',pixels));
 IF p_task->'execution_profile' IS DISTINCT FROM profile THEN RETURN false;END IF;
 refs_text:='';
 FOR ref IN SELECT value FROM jsonb_array_elements(inputs) LOOP
  IF ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  IF refs_text<>'' THEN refs_text:=refs_text||',';END IF;
  refs_text:=refs_text||format('{"artifact_id":"%s","sha256":"%s"}',(ref->>'artifact_id')::uuid,ref->>'sha256');
 END LOOP;
 digest:=encode(sha256(convert_to(format('{"inputs":[%s],"operation":"media.preview","operation_version":"1","params":{"artifact_id":"%s","sha256":"%s"},"version":"task_input_v1"}',refs_text,aid,d.sha256),'UTF8')),'hex');
 IF p_task->>'input_digest' IS DISTINCT FROM digest THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=task_id;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=p_task->>'node_key' AND old.operation='media.preview'
 AND old.operation_version='1' AND old.resource_class='media' AND old.params_artifact_id=aid
 AND old.inputs=inputs AND old.input_digest=digest AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(task_id,p_tenant,p_run,p_task->>'node_key','media.preview','1','media','blocked',inputs,aid,digest,profile);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_v26_render_derivative_task(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v26_render_derivative_task(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_v26_render_derivative_task(uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
