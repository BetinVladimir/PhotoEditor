-- +goose Up
-- Reopen the sealed encoded child only after independent encode verification.
-- Public admission must freeze metadata policy/approvals before this transition.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v30_metadata_barrier(p_tenant uuid,p_child uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_params text,p_source text,p_encoded text,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;parent photo.runs%ROWTYPE;
 e photo.graph_expansions%ROWTYPE;existing photo.graph_expansions%ROWTYPE;frozen jsonb;request jsonb;policy text;slot integer;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR p_encoded IS NULL OR octet_length(p_encoded) NOT BETWEEN 2 AND 1048576
 OR p_bytes IS NULL OR p_bytes<1 THEN RETURN false;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 IF NOT FOUND THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';policy:=((request->>'config_jcs')::jsonb)->>'metadata_policy';
 slot:=(request->>'slot_index')::integer;
 IF policy IS NULL OR policy NOT IN('copyright','approved_exif') OR slot NOT BETWEEN 0 AND 79999
 OR r.id<>photo.v30_uuid5((request->>'parent_run_id')::uuid,'child:export:'||slot::text) THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(p_child,(request->>'parent_run_id')::uuid) ORDER BY id FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=(request->>'parent_run_id')::uuid;
 IF r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR r.revision>=9007199254740991
 OR parent.id IS NULL OR parent.kind<>'v2' OR parent.status<>'running' OR parent.cancel_requested
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=parent.id AND kind='v2.delivery/1' AND state='open' FOR UPDATE;
 IF NOT FOUND OR slot>=e.cursor::integer OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen FROM photo.backend_documents b
 JOIN photo.tasks prepare ON prepare.tenant_id=b.tenant_id AND prepare.id=b.owner_id
 WHERE b.tenant_id=p_tenant AND b.run_id=parent.id AND prepare.run_id=parent.id
 AND prepare.operation='v2.delivery.prepare' AND b.artifact_id=prepare.params_artifact_id
 AND b.purpose='task_params' AND b.state='ready' FOR SHARE OF b;
 IF frozen IS NULL OR frozen->'policy'->>'metadata_policy' IS DISTINCT FROM policy
 OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'off' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child
 AND id=photo.v30_uuid5(photo.v30_uuid5(p_child,'workflow'),'export.encode:0') FOR UPDATE;
 IF NOT FOUND OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result OR p_result IS NULL THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,p_result)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
 AND a.sha256=encode(sha256(convert_to(p_params,'UTF8')),'hex') AND a.bytes=octet_length(p_params))
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id
 AND i.artifact_id=((p_params::jsonb)->'image'->>'artifact_id')::uuid
 AND i.sha256=encode(sha256(convert_to(p_source,'UTF8')),'hex'))
 OR NOT EXISTS(SELECT 1 FROM photo.task_results result CROSS JOIN LATERAL jsonb_array_elements(result.outputs) output
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
 WHERE result.tenant_id=p_tenant AND result.task_id=t.id AND result.id=p_result AND a.output_role='encoded_manifest'
 AND a.sha256=encode(sha256(convert_to(p_encoded,'UTF8')),'hex') AND a.bytes=octet_length(p_encoded)) THEN RETURN false;END IF;
 SELECT * INTO existing FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND kind='v30.delivery.metadata/1' FOR UPDATE;
 IF FOUND THEN
  RETURN existing.id=photo.v30_uuid5(p_child,'delivery:metadata') AND existing.parent_task_id=t.id
  AND existing.state='open' AND existing.expected_count=1 AND NOT r.graph_sealed
  AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
  AND event_id=photo.v2_adoption_event_id(t.id,p_result));
 END IF;
 IF NOT r.graph_sealed OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND state<>'closed') THEN RETURN false;END IF;
 IF photo.record_v30_delivery_encode_verification(p_tenant,t.id,p_attempt,p_fence,p_result,p_params,p_source,p_encoded,p_bytes) IS DISTINCT FROM true THEN RETURN false;END IF;
 INSERT INTO photo.graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count)
 VALUES(photo.v30_uuid5(p_child,'delivery:metadata'),p_tenant,p_child,t.id,'v30.delivery.metadata/1','open','0',1);
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_child;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
