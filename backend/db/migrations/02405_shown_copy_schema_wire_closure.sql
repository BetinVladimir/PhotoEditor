-- +goose Up
-- Reader-first copy-wire rollout: retain historical numeric documents unchanged.
-- Pure compatibility is separate from receipt, lease and live source authority.
-- +goose StatementBegin
CREATE FUNCTION photo.shown_copy_schema_wire_ready(p_schema text,p_params jsonb)
RETURNS boolean LANGUAGE sql IMMUTABLE
SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_params)='object'
 AND photo.shown_copy_fence_wire_value(p_params->'preview_fence') IS NOT NULL
 AND (p_schema='7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0'
 OR (p_schema='886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90' AND jsonb_typeof(p_params->'preview_fence')='number')),false);
$$;
ALTER FUNCTION photo.shown_copy_schema_wire_ready(text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.shown_copy_schema_wire_ready(text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_semantic_admission;
-- +goose StatementEnd

-- Preserve latest bounded transport reconstruction and every authority gate.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_retouch_closure_ready(p_task uuid,p_params jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;entries jsonb;roots jsonb;
 chunks jsonb;manifest jsonb;node jsonb;transport jsonb;inventory jsonb;profile jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind<>'apply' OR r.cancel_requested OR t.cancel_requested
 OR r.status NOT IN('draft','queued','running','completed') OR t.operation<>'semantic.adopt_shown_retouch_region'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR photo.shown_copy_schema_wire_ready(t.execution_profile->>'contracts_sha',p_params) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 profile:=t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha';
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' OR p_params->>'tenant_id' IS DISTINCT FROM t.tenant_id::text
 OR p_params->>'child_run_id' IS DISTINCT FROM r.id::text OR profile->>'backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=p_params->>'target_id' AND n->>'params_artifact_id'=t.params_artifact_id::text)<>1 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract) ORDER BY i.ordinal),
 jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal)
 INTO entries,roots FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.ordinal<1000000;
 -- Params is still registered and included in both inventory digests. At
 -- exactly 512 entries it travels separately in TaskCommand.params, avoiding
 -- a 513-ref input envelope. Retain old roots for all smaller inventories.
 IF jsonb_array_length(entries)=512 THEN
  SELECT jsonb_agg(e.value->'ref' ORDER BY e.ordinality) INTO roots
  FROM jsonb_array_elements(entries) WITH ORDINALITY e(value,ordinality)
  WHERE e.value->>'contract'<>'Params';
 END IF;
 IF entries IS NULL OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id)
 IS DISTINCT FROM jsonb_array_length(entries)+2
 OR EXISTS(SELECT 1 FROM (SELECT ordinal,row_number() OVER(ORDER BY ordinal)-1 AS expected
 FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id AND ordinal<1000000) direct
 WHERE direct.ordinal<>direct.expected)
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
 AND i.project_id::text IS DISTINCT FROM r.config_snapshot->>'project_id') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND (a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256
 OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal)
 INTO chunks FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id
 AND contract='InputClosureChunk' AND ordinal=1000000;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO manifest
 FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id
 AND contract='InputClosureManifest' AND ordinal=1000001;
 node:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
 'resource_class',t.resource_class,'params_artifact_id',t.params_artifact_id,
 'params_sha',(SELECT n->>'params_sha' FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n WHERE n->>'task_id'=t.id::text),'input_digest',t.input_digest,'execution_profile',profile);
 transport:=jsonb_build_object('task_id',t.id,'entry_count',t.execution_profile->'v2_entry_count',
 'entries_sha',t.execution_profile->>'v2_entries_sha','manifest',manifest,'chunks',chunks,'direct_roots',roots);
 inventory:=jsonb_build_object('task_id',t.id,'entries',entries);
 RETURN coalesce(t.inputs=jsonb_build_array(manifest)||roots
 AND t.execution_profile->'v2_input_manifest'=manifest
 AND photo.semantic_shown_retouch_transport_ready(r.id,p_params,
 node,transport,inventory) IS TRUE,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_closure_ready(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_closure_ready(uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
