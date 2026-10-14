-- +goose Up
-- Preserve verified encode evidence for metadata materialization/recovery.
-- Historical barriers are not backfilled with inferred object bytes.
ALTER TABLE photo.graph_expansions ADD COLUMN v30_metadata_encoded_jcs text;
ALTER TABLE photo.graph_expansions ADD COLUMN v30_metadata_encoded_sha text;
ALTER TABLE photo.graph_expansions ADD CONSTRAINT v30_metadata_encoded_pair CHECK(
 (v30_metadata_encoded_jcs IS NULL AND v30_metadata_encoded_sha IS NULL) OR
 (kind='v30.delivery.metadata/1' AND v30_metadata_encoded_jcs IS NOT NULL AND v30_metadata_encoded_sha IS NOT NULL
 AND octet_length(v30_metadata_encoded_jcs) BETWEEN 2 AND 1048576
 AND v30_metadata_encoded_sha=encode(sha256(convert_to(v30_metadata_encoded_jcs,'UTF8')),'hex')));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_metadata_encoded_evidence() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.v30_metadata_encoded_jcs IS NOT NULL AND
 (NEW.v30_metadata_encoded_jcs IS DISTINCT FROM OLD.v30_metadata_encoded_jcs OR NEW.v30_metadata_encoded_sha IS DISTINCT FROM OLD.v30_metadata_encoded_sha) THEN
 RAISE EXCEPTION 'IMMUTABLE_METADATA_ENCODED_EVIDENCE' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_metadata_encoded_evidence() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_metadata_encoded_evidence() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE TRIGGER immutable_v30_metadata_encoded_evidence BEFORE UPDATE OF v30_metadata_encoded_jcs,v30_metadata_encoded_sha ON photo.graph_expansions
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_metadata_encoded_evidence();
DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)'::regprocedure) IS DISTINCT FROM '547bf29e75484039a743f30c8647ea92' THEN RAISE EXCEPTION 'METADATA_ENCODED_EVIDENCE_DEFINITION_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.begin_v30_metadata_barrier(p_tenant uuid,p_child uuid,p_attempt integer,p_fence bigint,p_result uuid,
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
  RETURN existing.v30_metadata_encoded_jcs=p_encoded AND existing.v30_metadata_encoded_sha=encode(sha256(convert_to(p_encoded,'UTF8')),'hex') AND existing.id=photo.v30_uuid5(p_child,'delivery:metadata') AND existing.parent_task_id=t.id
  AND existing.state='open' AND existing.expected_count=1 AND NOT r.graph_sealed
  AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
  AND event_id=photo.v2_adoption_event_id(t.id,p_result));
 END IF;
 IF NOT r.graph_sealed OR EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND state<>'closed') THEN RETURN false;END IF;
 IF photo.record_v30_delivery_encode_verification(p_tenant,t.id,p_attempt,p_fence,p_result,p_params,p_source,p_encoded,p_bytes) IS DISTINCT FROM true THEN RETURN false;END IF;
 INSERT INTO photo.graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count,v30_metadata_encoded_jcs,v30_metadata_encoded_sha)
 VALUES(photo.v30_uuid5(p_child,'delivery:metadata'),p_tenant,p_child,t.id,'v30.delivery.metadata/1','open','0',1,p_encoded,encode(sha256(convert_to(p_encoded,'UTF8')),'hex'));
 UPDATE photo.runs SET graph_sealed=false,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_child;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
CREATE FUNCTION photo.read_v30_metadata_stage_input(p_tenant uuid,p_child uuid,p_expansion uuid,p_token uuid)
RETURNS TABLE(child_config text,encoded_artifact uuid,encoded_jcs text,encoded_bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;
 manifest jsonb;envelope jsonb;proof jsonb;source_text text;source_sha text;approved text[];size bigint;manifest_id uuid;request jsonb;policy text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_token IS NULL OR photo.v30_metadata_barrier_ready(p_tenant,p_child) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND id=p_expansion
 AND kind='v30.delivery.metadata/1' AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND OR e.v30_metadata_encoded_jcs IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 request:=r.config_snapshot->'v30_delivery_export';policy:=((request->>'config_jcs')::jsonb)->>'metadata_policy';
 IF jsonb_typeof(r.config_snapshot->'v30_metadata_approved_fields') IS DISTINCT FROM 'array' THEN RETURN;END IF;
 SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
 FROM jsonb_array_elements_text(r.config_snapshot->'v30_metadata_approved_fields');
 SELECT publication_jcs,publication_sha INTO source_text,source_sha FROM photo.read_v30_metadata_source(p_tenant,
 (request->>'parent_run_id')::uuid,request->'binding',r.config_snapshot->'v30_metadata_source',policy,approved);
 IF source_text IS NULL THEN RETURN;END IF;
 envelope:=source_text::jsonb;
 proof:=jsonb_build_object('metadata_source',envelope->'metadata_source','metadata_jcs',envelope->>'metadata_jcs',
 'binding',request->'binding','policy',policy,'frozen_jcs',envelope->>'frozen_jcs','frozen_sha256',envelope->>'frozen_sha256');
 IF photo.v30_metadata_child_source_ready(p_tenant,p_child,r.config_snapshot,proof,approved) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=e.parent_task_id AND run_id=p_child;
 SELECT * INTO z FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=t.id AND id=t.canonical_result_id
 AND attempt=t.attempt AND fencing_token=t.fencing_token;
 IF NOT FOUND THEN RETURN;END IF;
 manifest:=e.v30_metadata_encoded_jcs::jsonb;
 SELECT a.id INTO manifest_id FROM jsonb_array_elements(z.outputs) output JOIN photo.artifacts a
 ON a.tenant_id=p_tenant AND a.id=(output->>'artifact_id')::uuid AND a.sha256=output->>'sha256'
 WHERE a.sha256=e.v30_metadata_encoded_sha AND a.bytes=octet_length(e.v30_metadata_encoded_jcs)
 AND a.output_role='encoded_manifest' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF manifest_id IS NULL THEN RETURN;END IF;
 SELECT a.bytes INTO size FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id=(manifest->'artifact'->>'artifact_id')::uuid AND a.sha256=manifest->'artifact'->>'sha256'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='encoded_image'
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND z.outputs @> jsonb_build_array(manifest->'artifact');
 IF size IS NULL OR size NOT BETWEEN 1 AND 2147483648 THEN RETURN;END IF;
 RETURN QUERY SELECT r.config_snapshot::text,manifest_id,e.v30_metadata_encoded_jcs,size;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_metadata_stage_input(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_metadata_stage_input(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_metadata_stage_input(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
