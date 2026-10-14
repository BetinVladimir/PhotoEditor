-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_resources_ready(p_profile jsonb,p_width bigint,p_height bigint,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE resource jsonb;name text;pixels numeric;
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_width<1 OR p_height<1 OR p_width::numeric*p_height>200000000
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 2147483648
 OR p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 resource:=p_profile->'resource_estimate';pixels:=p_width::numeric*p_height;
 IF jsonb_typeof(resource) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(resource))<>5
 OR resource-'ram_bytes'-'vram_bytes'-'scratch_bytes'-'output_bytes'-'pixel_count'<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH name IN ARRAY ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'] LOOP
  IF jsonb_typeof(resource->name) IS DISTINCT FROM 'number' OR (resource->>name) !~ '^(0|[1-9][0-9]{0,15})$'
  OR (resource->>name)::numeric>9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 RETURN resource->'vram_bytes'='0'::jsonb AND (resource->>'pixel_count')::numeric=pixels
 AND (resource->>'ram_bytes')::numeric>=268435456+pixels*64+p_bytes::numeric*4
 AND (resource->>'scratch_bytes')::numeric>=p_bytes::numeric*3+8388608
 AND (resource->>'output_bytes')::numeric=p_bytes::numeric+2097152;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_resources_ready(jsonb,bigint,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_resources_ready(jsonb,bigint,bigint,bigint) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.v30_metadata_profile_ready(p_tenant uuid,p_expansion uuid,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;image jsonb;size bigint;
BEGIN
 SELECT v30_metadata_proof_jcs::jsonb INTO proof FROM photo.graph_expansions
 WHERE tenant_id=p_tenant AND id=p_expansion AND kind='v30.delivery.metadata/1';
 IF proof IS NULL THEN RETURN false;END IF;
 image:=(proof->>'encoded_jcs')::jsonb;
 SELECT bytes INTO size FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(image->'artifact'->>'artifact_id')::uuid
 AND sha256=image->'artifact'->>'sha256' AND state='ready' AND verified_at IS NOT NULL
 AND object_version IS NOT NULL AND object_version NOT IN('','null') AND (expires_at IS NULL OR expires_at>clock_timestamp());
 IF size IS NULL THEN RETURN false;END IF;
 RETURN photo.v30_metadata_resources_ready(p_profile,(image->>'width')::bigint,(image->>'height')::bigint,size);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_profile_ready(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_profile_ready(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.create_v30_metadata_task(uuid,uuid,uuid,jsonb,bigint)'::regprocedure) IS DISTINCT FROM 'cb3294a76c389040e64921b83a557e13' THEN RAISE EXCEPTION 'METADATA_RESOURCE_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.create_v30_metadata_task(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb,p_reserved bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof_text text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 owner uuid;ref jsonb;used numeric;capacity numeric;estimate numeric;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_reserved IS NULL OR p_reserved NOT BETWEEN 1 AND 9007199254740991
 OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active';
 IF used IS NULL OR capacity IS NULL THEN RETURN false;END IF;
 SELECT proof_jcs,proof_sha INTO proof_text,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF proof_text IS NULL THEN RETURN false;END IF;
 proof:=proof_text::jsonb;owner:=(proof->>'task_id')::uuid;
 IF photo.v30_metadata_profile_ready(p_tenant,p_expansion,p_task->'execution_profile') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id;
 IF r.id IS NULL OR r.graph_sealed OR r.status<>'running' OR r.cancel_requested THEN RETURN false;END IF;
 IF p_task->>'id' IS DISTINCT FROM owner::text OR p_task->>'node_key' IS DISTINCT FROM 'metadata.write:0'
 OR p_task->>'operation' IS DISTINCT FROM 'metadata.write' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_task->>'params_artifact_id' IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM proof->'params'->>'sha256'
 OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR p_task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu'
 OR p_task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_task->'execution_profile'->'resource_estimate'->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 estimate:=(p_task->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric;
 IF estimate IS NULL OR estimate<>p_reserved THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'params',proof->'projection'] LOOP
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
  WHERE b.tenant_id=p_tenant AND b.run_id=r.id AND b.owner_id=owner AND b.artifact_id=(ref->>'artifact_id')::uuid
  AND b.sha256=ref->>'sha256' AND b.state='ready'
  AND b.purpose=CASE WHEN ref=proof->'params' THEN 'task_params' ELSE 'v30_metadata_projection' END
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket
  AND a.object_version=b.object_version AND a.object_version NOT IN('','null')
  AND a.media_type='application/json' AND a.output_role=b.purpose
  AND a.object_key='tenants/'||p_tenant||'/runs/'||r.id||'/documents/'||b.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=owner FOR UPDATE;
 IF FOUND THEN RETURN e.expansion_lease_until>clock_timestamp() AND old.run_id=r.id AND old.state='blocked' AND old.attempt=0
 AND old.operation='metadata.write' AND old.operation_version='1' AND old.node_key='metadata.write:0'
 AND old.resource_class='cpu' AND old.params_artifact_id=(proof->'params'->>'artifact_id')::uuid
 AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=p_task->'execution_profile'
 AND EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id
 AND prerequisite_id=e.parent_task_id AND dependent_id=owner AND condition='success');END IF;
 IF used+p_reserved>capacity OR used+p_reserved>9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.bytes::numeric+p_reserved>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(owner,p_tenant,r.id,'metadata.write:0','metadata.write','1','cpu','blocked','[]'::jsonb,
 (proof->'params'->>'artifact_id')::uuid,p_task->>'input_digest',p_task->'execution_profile');
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
 VALUES(p_tenant,r.id,e.parent_task_id,owner,'success');
 UPDATE photo.quota_reservations SET bytes=bytes+p_reserved WHERE id=q.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserved WHERE tenant_id=p_tenant;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_transport_ready(uuid,uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM '692c0b79918650c90180cd6752b71a95' THEN RAISE EXCEPTION 'METADATA_RESOURCE_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_metadata_transport_ready(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 ref jsonb;expected jsonb;d photo.backend_documents%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 SELECT proof_jcs,proof_sha INTO body,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF body IS NULL THEN RETURN false;END IF;
 proof:=body::jsonb;
 IF photo.v30_metadata_profile_ready(p_tenant,p_expansion,p_task->'execution_profile') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=e.run_id AND id=(proof->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested OR t.operation<>'metadata.write'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.node_key<>'metadata.write:0'
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM p_task->>'operation' OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.params_artifact_id::text IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM proof->'params'->>'sha256'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=jsonb_build_array(proof->'params',proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact',proof->'projection');
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id
 AND contract NOT IN('InputClosureChunk','InputClosureManifest'))<>4 THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(expected) LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
  AND i.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND (ref<>proof->'params' OR i.contract='Params')) THEN RETURN false;END IF;
 END LOOP;
 FOR ref IN SELECT value FROM jsonb_array_elements(jsonb_build_array(proof->'params',proof->'projection')) LOOP
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=e.run_id AND owner_id=t.id
  AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND state='ready'
  AND purpose=CASE WHEN ref=proof->'params' THEN 'task_params' ELSE 'v30_metadata_projection' END;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=e.run_id
 AND prerequisite_id=e.parent_task_id AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_task_activated(uuid,uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM '57f550631440c759ca71adc5b4fc6ac2' THEN RAISE EXCEPTION 'METADATA_RESOURCE_DEFINITION_DRIFT';END IF;END $$;

CREATE OR REPLACE FUNCTION photo.v30_metadata_task_activated(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 ref jsonb;expected jsonb;d photo.backend_documents%ROWTYPE;r photo.runs%ROWTYPE;approved text[];
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.state<>'closed' OR e.cursor<>'1'
 OR e.expected_count<>1 OR p_token IS NULL OR e.expansion_token IS DISTINCT FROM p_token
 OR e.v30_metadata_proof_jcs IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR r.status NOT IN('running','waiting_review','completed')
 OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 proof:=e.v30_metadata_proof_jcs::jsonb;
 IF photo.v30_metadata_profile_ready(p_tenant,p_expansion,p_task->'execution_profile') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF proof->'binding' IS DISTINCT FROM r.config_snapshot->'v30_delivery_export'->'binding'
 OR proof->>'policy' IS DISTINCT FROM ((r.config_snapshot->'v30_delivery_export'->>'config_jcs')::jsonb)->>'metadata_policy'
 OR proof->>'task_id' IS DISTINCT FROM photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'metadata.write:0')::text THEN RETURN false;END IF;
 IF proof->>'policy'='approved_exif' THEN
  IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
  FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 ELSE approved:=ARRAY[]::text[];END IF;
 IF proof->'approved_fields' IS DISTINCT FROM to_jsonb(approved)
 OR photo.v30_metadata_write_plan_valid((proof->>'task_id')::uuid,proof->'encoded',proof->>'encoded_jcs',
 proof->>'frozen_jcs',proof->>'frozen_sha256',proof->'binding',proof->'metadata_source',proof->>'metadata_jcs',
 proof->>'policy',approved,proof->>'projection_jcs',proof->>'params_jcs') IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=e.run_id AND id=(proof->>'task_id')::uuid FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped') OR t.cancel_requested OR t.operation<>'metadata.write'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.node_key<>'metadata.write:0'
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM p_task->>'operation' OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.params_artifact_id::text IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM proof->'params'->>'sha256'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 expected:=jsonb_build_array(proof->'params',proof->'encoded',(proof->>'encoded_jcs')::jsonb->'artifact',proof->'projection');
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=t.id
 AND contract NOT IN('InputClosureChunk','InputClosureManifest'))<>4 THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(expected) LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
  AND i.sha256=ref->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND (ref<>proof->'params' OR i.contract='Params')) THEN RETURN false;END IF;
 END LOOP;
 FOR ref IN SELECT value FROM jsonb_array_elements(jsonb_build_array(proof->'params',proof->'projection')) LOOP
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=e.run_id AND owner_id=t.id
  AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND state='ready'
  AND purpose=CASE WHEN ref=proof->'params' THEN 'task_params' ELSE 'v30_metadata_projection' END;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=e.run_id
 AND prerequisite_id=e.parent_task_id AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.tasks predecessor WHERE predecessor.tenant_id=p_tenant
 AND predecessor.id=e.parent_task_id AND predecessor.run_id=e.run_id AND predecessor.operation='export.encode'
 AND predecessor.state='succeeded' AND NOT predecessor.cancel_requested AND predecessor.canonical_result_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
 AND event_id=photo.v2_adoption_event_id(predecessor.id,predecessor.canonical_result_id)));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
