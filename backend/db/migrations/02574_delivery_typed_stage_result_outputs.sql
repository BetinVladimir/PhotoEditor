-- +goose Up
-- Delivery's CPU stages publish their typed envelopes and physical children,
-- unlike render.compose's single ImageResult root. Preserve the ordinary root
-- guard for every other run/operation; this is not a generic multi-output path.
-- +goose StatementBegin
CREATE FUNCTION photo.delivery_stage_result_outputs_ready(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;parent photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;request jsonb;expected_roles text[];roles text[];
 prepare photo.tasks%ROWTYPE;proof_document photo.backend_documents%ROWTYPE;
 frozen jsonb;snapshot jsonb;preset jsonb;config jsonb;source_item jsonb;expected jsonb;preset_count integer;
 item jsonb;role_name text;artifact photo.artifacts%ROWTYPE;upload photo.output_uploads%ROWTYPE;
 slot_number integer;item_ordinal integer;selected_preset uuid;seen uuid[]:=ARRAY[]::uuid[];
BEGIN
 IF p_tenant IS NULL OR p_task IS NULL OR p_attempt IS NULL OR p_fence IS NULL
 OR p_attempt NOT BETWEEN 1 AND 6 OR p_fence<1 OR jsonb_typeof(p_outputs) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_outputs) NOT BETWEEN 2 AND 4 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF t.id IS NULL OR r.id IS NULL OR r.kind<>'v2' OR r.cancel_requested OR t.cancel_requested
 OR NOT r.graph_sealed OR r.status NOT IN('queued','running') OR t.state NOT IN('running','succeeded')
 OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';
 IF jsonb_typeof(request) IS DISTINCT FROM 'object' OR request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR request->>'child_run_id' IS DISTINCT FROM r.id::text
 OR coalesce(request->>'slot_index','') !~ '^(0|[1-9][0-9]{0,4})$'
 OR coalesce(request->>'ordinal','') !~ '^(0|[1-9][0-9]{0,3})$' THEN RETURN false;END IF;
 slot_number:=(request->>'slot_index')::integer;item_ordinal:=(request->>'ordinal')::integer;
 selected_preset:=(request->'preset'->>'preset_id')::uuid;
 IF slot_number>79999 OR item_ordinal>4999 OR selected_preset IS NULL THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=(request->>'parent_run_id')::uuid;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=parent.id;
 IF parent.id IS NULL OR parent.kind<>'v2' OR parent.status<>'running' OR parent.cancel_requested
 OR NOT parent.graph_sealed OR parent.created_by IS DISTINCT FROM r.created_by OR parent.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR w.id IS NULL OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2'
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR r.id IS DISTINCT FROM photo.v30_uuid5(parent.id,'child:export:'||slot_number::text)
 OR r.config_snapshot->>'export_id' IS DISTINCT FROM photo.v30_uuid5(r.id,'v30:delivery-export:1')::text
 OR t.id IS DISTINCT FROM photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),t.operation||':0')
 OR t.node_key IS DISTINCT FROM t.operation||':0'
 OR NOT EXISTS(SELECT 1 FROM photo.delivery_packages d JOIN photo.delivery_items i
 ON i.tenant_id=d.tenant_id AND i.package_id=d.id
 WHERE d.tenant_id=p_tenant AND d.workflow_id=w.id AND d.project_id=w.project_id AND d.state='building'
 AND i.ordinal=item_ordinal AND i.preset_id=selected_preset AND i.preset_sha=request->'preset'->>'preset_sha'
 AND i.state='pending' AND i.export_id=photo.v30_uuid5(r.id,'v30:delivery-export:1')) THEN RETURN false;END IF;
 -- Reconstruct the complete frozen request from the admitted parent proof;
 -- no ephemeral graph expansion lease/token is needed for worker completion.
 SELECT * INTO prepare FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=parent.id
 AND id=photo.v30_uuid5(w.id,'v2.delivery.prepare:0') AND operation='v2.delivery.prepare'
 AND operation_version='1' AND state='succeeded' AND NOT cancel_requested AND canonical_result_id IS NOT NULL;
 SELECT * INTO proof_document FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=parent.id
 AND owner_id=prepare.id AND purpose='task_params' AND artifact_id=prepare.params_artifact_id AND state='ready';
 IF prepare.id IS NULL OR proof_document.id IS NULL OR proof_document.v30_delivery_proof IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=prepare.id
  AND z.id=prepare.canonical_result_id AND z.attempt=prepare.attempt AND z.fencing_token=prepare.fencing_token)
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=proof_document.artifact_id
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=proof_document.sha256
  AND a.bucket=proof_document.bucket AND a.object_version=proof_document.object_version
  AND a.bytes=proof_document.bytes AND a.media_type='application/json'
  AND a.object_key='tenants/'||p_tenant::text||'/runs/'||parent.id::text||'/documents/'||a.id::text
  AND coalesce(a.object_version,'') NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
 OR photo.v2_input_closure_ready(prepare.id) IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs input JOIN photo.artifacts a
  ON a.tenant_id=input.tenant_id AND a.id=input.artifact_id AND a.sha256=input.sha256
  WHERE input.tenant_id=p_tenant AND input.task_id=t.id AND input.artifact_id=t.params_artifact_id
  AND input.contract='Params' AND a.state='ready' AND a.verified_at IS NOT NULL
  AND coalesce(a.object_version,'') NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 frozen:=(proof_document.v30_delivery_proof->>'frozen_input_jcs')::jsonb;
 snapshot:=(proof_document.v30_delivery_proof->>'snapshot_jcs')::jsonb;
 IF jsonb_typeof(frozen->'presets') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 preset_count:=jsonb_array_length(frozen->'presets');
 IF preset_count NOT BETWEEN 1 AND 16 OR slot_number/preset_count<>item_ordinal THEN RETURN false;END IF;
 source_item:=snapshot->'items'->item_ordinal;
 SELECT value INTO preset FROM jsonb_array_elements(frozen->'presets') ORDER BY value->>'preset_id' OFFSET (slot_number%preset_count) LIMIT 1;
 SELECT value INTO config FROM jsonb_array_elements(proof_document.v30_delivery_proof->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id';
 expected:=jsonb_build_object('schema_version',1,'parent_run_id',parent.id,'child_run_id',r.id,
  'slot_index',slot_number,'ordinal',item_ordinal,'binding',source_item->'binding','recipe',source_item->'recipe',
  'snapshot_digest',frozen->>'snapshot_digest','preset',preset,'config_jcs',config->>'config_jcs');
 IF request IS DISTINCT FROM expected THEN RETURN false;END IF;
 CASE t.operation
 WHEN 'derivative.resize','derivative.sharpen','derivative.watermark' THEN
  expected_roles:=ARRAY['derivative_report','working_image','working_rgb'];
  IF jsonb_array_length(p_outputs)=4 THEN expected_roles:=ARRAY['derivative_report','working_alpha','working_image','working_rgb'];END IF;
 WHEN 'export.encode' THEN expected_roles:=ARRAY['encoded_image','encoded_manifest'];
 WHEN 'metadata.write' THEN expected_roles:=ARRAY['encoded_image','encoded_manifest','metadata_report'];
 ELSE RETURN false;
 END CASE;
 IF jsonb_array_length(p_outputs)<>cardinality(expected_roles) THEN RETURN false;END IF;
 FOREACH item IN ARRAY ARRAY(SELECT value FROM jsonb_array_elements(p_outputs)) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'artifact_id'-'sha256')<>'{}'::jsonb
  OR jsonb_typeof(item->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(item->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(item->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR (item->>'artifact_id')::uuid=ANY(seen) THEN RETURN false;END IF;
  SELECT * INTO artifact FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(item->>'artifact_id')::uuid;
  SELECT * INTO upload FROM photo.output_uploads WHERE tenant_id=p_tenant AND artifact_id=artifact.id;
  IF artifact.id IS NULL OR upload.id IS NULL OR artifact.sha256 IS DISTINCT FROM item->>'sha256'
  OR artifact.state<>'ready' OR artifact.verified_at IS NULL OR artifact.bytes<1
  OR coalesce(artifact.object_version,'') IN('','null')
  OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp())
  OR artifact.created_by_task_id IS DISTINCT FROM t.id OR artifact.produced_attempt IS DISTINCT FROM p_attempt
  OR artifact.produced_fence IS DISTINCT FROM p_fence OR NOT artifact.output_role=ANY(expected_roles)
  OR upload.state<>'ready' OR upload.completed_at IS NULL OR upload.task_id IS DISTINCT FROM t.id
  OR upload.attempt IS DISTINCT FROM p_attempt OR upload.fence IS DISTINCT FROM p_fence
  OR (upload.role,upload.bucket,upload.object_key,upload.object_version,upload.sha256,upload.bytes,upload.media_type)
   IS DISTINCT FROM (artifact.output_role,artifact.bucket,artifact.object_key,artifact.object_version,artifact.sha256,artifact.bytes,artifact.media_type)
  OR NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy WHERE policy.operation=t.operation
   AND policy.version=t.operation_version AND policy.role=artifact.output_role
   AND policy.media_types ? artifact.media_type AND artifact.bytes<=policy.max_bytes) THEN RETURN false;END IF;
  seen:=array_append(seen,artifact.id);roles:=array_append(roles,artifact.output_role);
 END LOOP;
 SELECT array_agg(value ORDER BY value COLLATE "C") INTO roles FROM unnest(roles) value;
 RETURN roles=expected_roles;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.delivery_stage_result_outputs_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.delivery_stage_result_outputs_ready(uuid,uuid,integer,bigint,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE source text;anchor text; BEGIN
 SELECT pg_get_functiondef(p.oid) INTO source FROM pg_proc p
 WHERE p.oid='photo.guard_v2_result_root()'::regprocedure AND p.prosecdef
 AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql');
 anchor:=$anchor$ IF run_row.kind<>'v2' THEN RETURN NEW; END IF;$anchor$;
 IF source IS NULL OR (length(source)-length(replace(source,anchor,'')))/length(anchor)<>1
 OR position('delivery_stage_result_outputs_ready' IN source)>0 THEN RAISE EXCEPTION 'DELIVERY_TYPED_RESULT_GUARD_SOURCE_SHAPE';END IF;
 EXECUTE replace(source,anchor,anchor||$branch$
 IF run_row.config_snapshot ? 'v30_delivery_export' AND task_row.operation IN
 ('derivative.resize','derivative.sharpen','derivative.watermark','export.encode','metadata.write') THEN
  IF photo.delivery_stage_result_outputs_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS DISTINCT FROM true THEN
   RAISE EXCEPTION 'DELIVERY_TYPED_RESULT_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
$branch$);
END $$;
-- +goose StatementEnd

-- Row-locking reads of the immutable signing closure require UPDATE on one
-- column in PostgreSQL. Only the existing SECURITY DEFINER function owner gets
-- that prerequisite; every actual UPDATE, including no-op, remains forbidden.
-- +goose StatementBegin
CREATE FUNCTION photo.reject_delivery_signing_closure_update() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN RAISE EXCEPTION 'DELIVERY_SIGNING_CLOSURE_IMMUTABLE' USING ERRCODE='23514';END $$;
ALTER FUNCTION photo.reject_delivery_signing_closure_update() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_delivery_signing_closure_update()
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
CREATE TRIGGER delivery_signing_closure_immutable BEFORE UPDATE ON photo.delivery_signing_closures
 FOR EACH ROW EXECUTE FUNCTION photo.reject_delivery_signing_closure_update();
GRANT UPDATE(closure_sha) ON photo.delivery_signing_closures TO photo_fn_owner;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
