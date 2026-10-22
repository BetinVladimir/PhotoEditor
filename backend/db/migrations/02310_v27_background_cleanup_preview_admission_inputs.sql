-- +goose Up
-- Full background_cleanup input provenance and quota lock gate, before task materialization.
-- Private physical readiness predicate. Live editor/source authority,
-- dependency provenance, quota and relational admission remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_documents_ready(p_tenant uuid,p_preview uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;item record;artifact photo.artifacts%ROWTYPE;document photo.backend_documents%ROWTYPE;
 expected_role text;expected_id uuid;expected_sha text;
BEGIN
 IF photo.semantic_background_cleanup_preview_transport_ready(p_preview,p_params,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN RETURN false;END IF;
 task:=(p_node->>'id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_preview
 AND r.kind='preview' AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND r.config_snapshot->'semantic_background_cleanup_preview'->'params'=p_params
 AND r.config_snapshot->'semantic_background_cleanup_preview'->>'params_sha'=p_node->>'params_sha'
 AND r.v27_preapply_preview_profile=p_node->'execution_profile') THEN RETURN false;END IF;
 FOR item IN
 SELECT e->'ref' AS ref,e->>'contract' AS contract FROM jsonb_array_elements(p_inventory->'entries') e
 UNION ALL SELECT p_transport->'chunks'->0,'InputClosureChunk'
 UNION ALL SELECT p_transport->'manifest','InputClosureManifest'
 LOOP
 expected_id:=(item.ref->>'artifact_id')::uuid;expected_sha:=item.ref->>'sha256';
 SELECT * INTO artifact FROM photo.artifacts WHERE tenant_id=p_tenant AND id=expected_id;
 IF artifact.id IS NULL OR artifact.sha256 IS DISTINCT FROM expected_sha OR artifact.state<>'ready'
 OR artifact.verified_at IS NULL OR artifact.object_version IS NULL OR artifact.object_version IN('','null')
 OR artifact.bytes IS NULL OR artifact.bytes NOT BETWEEN 1 AND 9007199254740991
 OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp())
 OR (item.contract='NpyArray' AND coalesce(artifact.media_type,'') NOT IN('application/x-npy','application/octet-stream'))
 OR (item.contract<>'NpyArray' AND artifact.media_type IS DISTINCT FROM 'application/json') THEN RETURN false;END IF;
 IF item.contract IN('Params','InputClosureChunk','InputClosureManifest') THEN
 expected_role:=CASE item.contract WHEN 'Params' THEN 'task_params'
 WHEN 'InputClosureChunk' THEN 'v2_input_chunk_0' ELSE 'v2_input_manifest' END;
 SELECT * INTO document FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
 AND d.run_id=p_preview AND d.owner_id=task AND d.purpose=expected_role;
 IF document.id IS NULL OR document.state<>'ready' OR document.artifact_id IS DISTINCT FROM expected_id
 OR document.sha256 IS DISTINCT FROM expected_sha OR document.bytes IS DISTINCT FROM artifact.bytes
 OR document.bucket IS DISTINCT FROM artifact.bucket OR document.object_version IS DISTINCT FROM artifact.object_version
 OR artifact.output_role IS DISTINCT FROM expected_role
 OR artifact.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/runs/'||p_preview||'/documents/'||expected_id THEN RETURN false;END IF;
 END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_documents_ready(uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private pre-lease protection provenance. Caller must separately lock current
-- authenticated preview/target and validate exact MaskRef/image_binary content.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_protection_ready(p_tenant uuid,p_project uuid,
 p_params jsonb,p_binary uuid,p_binary_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE edit jsonb:=p_params;p_root uuid;p_root_sha text;
BEGIN
 IF jsonb_typeof(edit) IS DISTINCT FROM 'object' OR p_project IS NULL
 OR p_binary IS NULL OR p_binary='00000000-0000-0000-0000-000000000000'::uuid
 OR p_binary_sha IS NULL OR p_binary_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 p_root:=(edit->'protection'->>'artifact_id')::uuid;p_root_sha:=edit->'protection'->>'sha256';
 IF p_root IS NULL OR p_root='00000000-0000-0000-0000-000000000000'::uuid
 OR p_root=p_binary OR p_root_sha IS NULL OR p_root_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts candidate
 JOIN photo.tasks producer ON producer.tenant_id=candidate.tenant_id AND producer.id=candidate.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.task_id=producer.id
 AND result.id=producer.canonical_result_id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.runs original ON original.tenant_id=producer.tenant_id AND original.id=producer.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=original.tenant_id AND w.run_id=original.id AND w.project_id=p_project
 JOIN photo.semantic_intents i ON i.tenant_id=w.tenant_id AND i.workflow_id=w.id
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id AND target.id=(edit->>'target_id')::uuid
 WHERE w.id::text=edit->>'workflow_id' AND i.id::text=edit->>'intent_id'
 AND candidate.tenant_id=p_tenant AND candidate.id=(edit->'candidate'->>'artifact_id')::uuid
 AND candidate.sha256=edit->'candidate'->>'sha256' AND candidate.produced_attempt=producer.attempt
 AND candidate.produced_fence=producer.fencing_token AND producer.state='succeeded'
 AND producer.operation='semantic.transfer' AND producer.operation_version='1'
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256))
 AND jsonb_typeof(original.config_snapshot->'v27_semantic'->'nodes')='array'
 AND (SELECT count(*) FROM jsonb_array_elements(original.config_snapshot->'v27_semantic'->'nodes') n
 WHERE n->>'task_id'=producer.id::text AND n->'target_protection'=jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))=1
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs input WHERE input.tenant_id=p_tenant AND input.project_id=p_project
 AND input.task_id=producer.id AND input.artifact_id=p_root AND input.sha256=p_root_sha AND input.contract='MaskRef')) THEN RETURN false;END IF;
 RETURN photo.semantic_protection_producer_ready(p_tenant,p_project,(edit->'source'->'source'->>'artifact_id')::uuid,
 edit->'source'->'source'->>'sha256',p_root,p_root_sha,p_binary,p_binary_sha);
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_protection_ready(uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_protection_ready(uuid,uuid,jsonb,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Candidate-child helpers below only lock a proposed review target; despite
-- historical names they neither approve an edit nor require an Apply child.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_sidecars_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_candidate_body text,p_mask_body text,p_transform_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;mask record;transform record;image_binary record;required record;
BEGIN
 PERFORM photo.lock_semantic_background_cleanup_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_candidate_body IS NULL OR octet_length(p_candidate_body) NOT BETWEEN 2 AND 1048576
 OR p_mask_body IS NULL OR octet_length(p_mask_body) NOT BETWEEN 2 AND 1048576
 OR p_transform_body IS NULL OR octet_length(p_transform_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR (p_candidate_body::jsonb)->'target' IS DISTINCT FROM e->'source' THEN RETURN false;END IF;
 SELECT * INTO mask FROM photo.semantic_approved_candidate_child(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,'mask');
 SELECT * INTO transform FROM photo.semantic_approved_candidate_child(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,'transform');
 SELECT * INTO image_binary FROM photo.semantic_approved_mask_binary(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,
 e->'candidate'->>'sha256',p_candidate_body,p_mask_body);
 IF mask.artifact_id IS NULL OR transform.artifact_id IS NULL OR image_binary.artifact_id IS NULL
 OR mask.artifact_id=transform.artifact_id OR mask.artifact_id=image_binary.artifact_id
 OR transform.artifact_id=image_binary.artifact_id OR mask.bytes<>octet_length(p_mask_body)
 OR transform.bytes<>octet_length(p_transform_body)
 OR encode(public.digest(convert_to(p_transform_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM transform.sha256 THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (mask.artifact_id,mask.sha256,'MaskRef'),(transform.artifact_id,transform.sha256,'ResolvedTransform'),
 (image_binary.artifact_id,image_binary.sha256,'NpyArray')) AS sidecars(id,sha,contract) LOOP
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=jsonb_build_object('artifact_id',required.id,'sha256',required.sha)
 AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_sidecars_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private proposed-target image/dependency provenance, without Apply approval.
-- Physical arrays, ICC and geometry require independent worker verification.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_images_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_intent_body text,p_source_body text,p_reference_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;intent jsonb;image jsonb;pin jsonb;raw text;root record;image_binary record;child record;is_reference boolean;
BEGIN
 PERFORM photo.lock_semantic_background_cleanup_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_intent_body IS NULL OR octet_length(p_intent_body) NOT BETWEEN 2 AND 1048576
 OR p_source_body IS NULL OR octet_length(p_source_body) NOT BETWEEN 2 AND 1048576
 OR p_reference_body IS NULL OR octet_length(p_reference_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR encode(public.digest(convert_to(p_intent_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'intent'->>'sha256'
 THEN RETURN false;END IF;
 intent:=p_intent_body::jsonb;
 IF intent->>'kind' IS DISTINCT FROM 'background_cleanup' OR intent->'ev' IS DISTINCT FROM 'null'::jsonb OR intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR intent->>'target_snapshot_id' IS DISTINCT FROM e->>'snapshot_id' THEN RETURN false;END IF;
 FOREACH is_reference IN ARRAY ARRAY[false,true] LOOP
 pin:=CASE WHEN is_reference THEN intent->'reference'->'source' ELSE e->'source'->'source' END;
 raw:=CASE WHEN is_reference THEN p_reference_body ELSE p_source_body END;
 SELECT * INTO root FROM photo.semantic_approved_image_object(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 is_reference,(pin->>'artifact_id')::uuid,pin->>'sha256');
 IF root.artifact_id IS NULL OR root.bytes<>octet_length(raw)
 OR encode(public.digest(convert_to(raw,'UTF8'),'sha256'),'hex') IS DISTINCT FROM root.sha256
 OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=pin AND item->>'contract'='WorkingImage') THEN RETURN false;END IF;
 image:=raw::jsonb;
 IF jsonb_typeof(image->'pixels') IS DISTINCT FROM 'object'
 OR jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(image->'transform_chain'->'steps')>32 THEN RETURN false;END IF;
 FOR child IN
 SELECT 'pixels'::text AS kind,-1 AS step
 UNION ALL SELECT 'alpha',-1 WHERE image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb
 UNION ALL SELECT 'inverse_field',(s.ordinality-1)::integer
 FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY s(value,ordinality)
 WHERE s.value->>'kind'='dense_warp'
 LOOP
 SELECT * INTO image_binary FROM photo.semantic_approved_image_binary(p_tenant,p_intent,p_actor,p_session,
 (e->>'workflow_revision')::bigint,p_target,(e->>'target_revision')::bigint,
 (e->'source'->>'asset_version_id')::uuid,(e->'candidate'->>'artifact_id')::uuid,e->'candidate'->>'sha256',
 is_reference,(pin->>'artifact_id')::uuid,pin->>'sha256',raw,child.kind,child.step);
 IF image_binary.artifact_id IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_inventory->'entries') item
 WHERE item->'ref'=jsonb_build_object('artifact_id',image_binary.artifact_id,'sha256',image_binary.sha256)
 AND item->>'contract'='NpyArray') THEN RETURN false;END IF;
 END LOOP;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure exact dependency projection. Caller must hash-bind every JSON document
-- and prove live authenticated producer/physical provenance before admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_inventory_exact(p_params jsonb,p_node jsonb,p_inventory jsonb,
 p_candidate jsonb,p_intent jsonb,p_source jsonb,p_reference jsonb,p_mask jsonb,p_protection jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;count_entries integer;unique_ids integer;
BEGIN
 IF jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' OR jsonb_typeof(p_reference) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_mask) IS DISTINCT FROM 'object' OR jsonb_typeof(p_protection) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_reference->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 WITH roots(ref,contract) AS (
 SELECT * FROM (VALUES
 (jsonb_build_object('artifact_id',p_node->>'params_artifact_id','sha256',p_node->>'params_sha'),'Params'),
 (p_params->'plan','SemanticPlan'),(p_params->'intent','EditIntent'),(p_params->'candidate','TransferCandidate'),
 (p_params->'source'->'source','WorkingImage'),(p_params->'protection','MaskRef'),
 (p_candidate->'mask','MaskRef'),(p_candidate->'transform','ResolvedTransform'),
 (p_intent->'reference'->'source','WorkingImage'),(p_source->'pixels','NpyArray'),
 (p_reference->'pixels','NpyArray'),(p_mask->'artifact','NpyArray'),(p_protection->'artifact','NpyArray')) AS fixed_roots(ref,contract)
 UNION ALL SELECT p_source->'alpha','NpyArray' WHERE p_source->'alpha' IS NOT NULL AND p_source->'alpha'<>'null'::jsonb
 UNION ALL SELECT p_reference->'alpha','NpyArray' WHERE p_reference->'alpha' IS NOT NULL AND p_reference->'alpha'<>'null'::jsonb
 UNION ALL SELECT step->'inverse_field','NpyArray' FROM jsonb_array_elements(p_source->'transform_chain'->'steps') step WHERE step->>'kind'='dense_warp'
 UNION ALL SELECT step->'inverse_field','NpyArray' FROM jsonb_array_elements(p_reference->'transform_chain'->'steps') step WHERE step->>'kind'='dense_warp'
 ),deduplicated AS (SELECT DISTINCT ref,contract FROM roots)
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract) ORDER BY (ref->>'artifact_id')::uuid),
 count(*)::integer,count(DISTINCT ref->>'artifact_id')::integer INTO expected,count_entries,unique_ids FROM deduplicated;
 IF count_entries NOT BETWEEN 1 AND 512 OR count_entries<>unique_ids
 OR expected IS DISTINCT FROM p_inventory->'entries' THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private aggregate admission gate. Caller must materialize and seal in the
-- same transaction while these authority/quota locks remain held.
-- This function grants no approval and creates no task.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_admission_inputs_ready(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;protection jsonb;project uuid;raw text;profile jsonb;
 quota photo.quota_reservations%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 PERFORM photo.lock_semantic_background_cleanup_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 FOREACH raw IN ARRAY ARRAY[p_candidate_body,p_intent_body,p_source_body,p_reference_body,
 p_mask_body,p_transform_body,p_protection_body] LOOP
 IF raw IS NULL OR octet_length(raw) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 END LOOP;
 e:=p_body::jsonb;protection:=p_protection_body::jsonb;
 IF photo.semantic_background_cleanup_preview_documents_ready(p_tenant,p_preview,e,p_node,p_transport,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_background_cleanup_preview_sidecars_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_mask_body,p_transform_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_background_cleanup_preview_images_ready(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_intent_body,p_source_body,p_reference_body,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_background_cleanup_preview_inventory_exact(e,p_node,p_inventory,p_candidate_body::jsonb,
 p_intent_body::jsonb,p_source_body::jsonb,p_reference_body::jsonb,p_mask_body::jsonb,protection) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF protection->'protected' IS DISTINCT FROM 'true'::jsonb
 OR encode(public.digest(convert_to(p_protection_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'protection'->>'sha256'
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts WHERE tenant_id=p_tenant
 AND id=(e->'protection'->>'artifact_id')::uuid AND sha256=e->'protection'->>'sha256'
 AND bytes=octet_length(p_protection_body)) THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 IF photo.semantic_background_cleanup_preview_protection_ready(p_tenant,project,e,
 (protection->'artifact'->>'artifact_id')::uuid,protection->'artifact'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 profile:=p_node->'execution_profile';
 IF photo.v27_downstream_profile_ready(p_tenant,project,'semantic.preview_background_cleanup','1','gpu',profile) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 -- Preserve tenant -> parent -> target -> preview -> usage -> quota lock order.
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO quota FROM photo.quota_reservations WHERE tenant_id=p_tenant
 AND holder_type='run' AND holder_id=p_preview FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN coalesce(quota.state='reserved' AND quota.principal_type='user' AND quota.principal_id=p_actor
 AND quota.share_id IS NULL AND quota.files=0 AND quota.bytes=(profile->'resource_estimate'->>'output_bytes')::bigint
 AND quota.expires_at>clock_timestamp(),false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_admission_inputs_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
