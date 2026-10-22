-- +goose Up
-- BackgroundCleanup copy document/profile preflight; task activation remains separate.
-- Pure exact dependency projection. Caller must hash-bind every JSON document
-- and prove live authenticated producer/physical provenance before admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_inventory_exact(p_adoption jsonb,p_presentation jsonb,p_params jsonb,p_node jsonb,p_inventory jsonb,
 p_candidate jsonb,p_intent jsonb,p_source jsonb,p_reference jsonb,p_mask jsonb,p_protection jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;count_entries integer;unique_ids integer;
BEGIN
 IF jsonb_typeof(p_adoption) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_presentation) IS DISTINCT FROM 'object'
 OR p_presentation->'params' IS DISTINCT FROM p_params
 OR p_presentation->'working_image' IS DISTINCT FROM p_adoption->'working_image'
 OR p_presentation->'pixels' IS DISTINCT FROM p_adoption->'pixels'
 OR p_presentation->'numerical_evidence' IS DISTINCT FROM p_adoption->'numerical_evidence'
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object' OR jsonb_typeof(p_reference) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_mask) IS DISTINCT FROM 'object' OR jsonb_typeof(p_protection) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_source->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_typeof(p_reference->'transform_chain'->'steps') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 WITH roots(ref,contract) AS (
 SELECT * FROM (VALUES
 (jsonb_build_object('artifact_id',p_node->>'params_artifact_id','sha256',p_node->>'params_sha'),'Params'),
 (p_adoption->'approved_edit','ApprovedSemanticEdit'),
 (p_adoption->'presentation','BackgroundCleanupPreviewPresentation'),
 (p_adoption->'numerical_evidence','BackgroundCleanupPreviewNumericalEvidence'),
 (p_adoption->'working_image','WorkingImage'),(p_adoption->'pixels','NpyArray'),
 (p_presentation->'before','EncodedImage'),(p_presentation->'after','EncodedImage'),
 (p_presentation->'affected','EncodedImage'),(p_presentation->'protected','EncodedImage'),
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
ALTER FUNCTION photo.semantic_shown_background_cleanup_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_inventory_exact(jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Physical metadata gate only. Exact inventory, producer provenance, publication
-- scope and active tenant/Apply authority must be verified by atomic admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_input_objects_ready(p_tenant uuid,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE entry jsonb;ref jsonb;contract_name text;aid uuid;prior uuid;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 512 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=entry->'ref';contract_name:=entry->>'contract';
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR NOT(entry ?& ARRAY['ref','contract'])
  OR (entry-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(entry->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef',
  'ResolvedTransform','NpyArray','ApprovedSemanticEdit','BackgroundCleanupPreviewPresentation','BackgroundCleanupPreviewNumericalEvidence','EncodedImage')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  prior:=aid;
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid;
  IF NOT FOUND OR a.sha256 IS DISTINCT FROM ref->>'sha256' OR a.state IS DISTINCT FROM 'ready'
  OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
  OR coalesce(a.bucket,'')='' OR coalesce(a.object_key,'')=''
  OR a.bytes IS NULL OR a.bytes NOT BETWEEN 1 AND 9007199254740991
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
  IF contract_name='NpyArray' THEN
   IF coalesce(a.media_type,'') NOT IN('application/x-npy','application/octet-stream') OR a.bytes>2400016384 THEN RETURN false;END IF;
  ELSIF contract_name='EncodedImage' THEN
   IF a.media_type IS DISTINCT FROM 'image/png' OR a.bytes>33882112 THEN RETURN false;END IF;
  ELSE
   IF a.media_type IS DISTINCT FROM 'application/json' OR a.bytes>1048576 THEN RETURN false;END IF;
  END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_input_objects_ready(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_input_objects_ready(uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private transport readiness gate. document_run is the server-selected durable
-- publication scope (parent before child creation, or child after creation).
-- Caller must prove this scope in the atomic Apply transaction; this function
-- alone grants neither cross-run reads nor permission to activate tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_transport_documents_ready(p_tenant uuid,p_document_run uuid,p_child uuid,
 p_params jsonb,p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;item record;aid uuid;digest text;a photo.artifacts%ROWTYPE;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_tenant='00000000-0000-0000-0000-000000000000'::uuid
 OR p_document_run IS NULL OR p_document_run='00000000-0000-0000-0000-000000000000'::uuid
 OR photo.semantic_shown_background_cleanup_transport_ready(p_child,p_params,p_node,p_transport,p_inventory) IS DISTINCT FROM true
 OR photo.semantic_shown_background_cleanup_input_objects_ready(p_tenant,p_inventory->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 task:=(p_node->>'id')::uuid;
 FOR item IN SELECT * FROM (VALUES
 (jsonb_build_object('artifact_id',p_node->>'params_artifact_id','sha256',p_node->>'params_sha'),'task_params'),
 (p_transport->'chunks'->0,'v2_input_chunk_0'),(p_transport->'manifest','v2_input_manifest')) AS docs(ref,purpose)
 LOOP
  aid:=(item.ref->>'artifact_id')::uuid;digest:=item.ref->>'sha256';
  SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=aid;
  IF NOT FOUND OR a.sha256 IS DISTINCT FROM digest OR a.state IS DISTINCT FROM 'ready'
  OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
  OR a.bytes IS NULL OR a.bytes NOT BETWEEN 1 AND 1048576
  OR a.media_type IS DISTINCT FROM 'application/json' OR coalesce(a.bucket,'')=''
  OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  OR a.output_role IS DISTINCT FROM item.purpose
  OR a.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/runs/'||p_document_run||'/documents/'||aid THEN RETURN false;END IF;
  SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_document_run
  AND owner_id=task AND purpose=item.purpose;
  IF NOT FOUND OR d.state IS DISTINCT FROM 'ready' OR d.artifact_id IS DISTINCT FROM aid
  OR d.sha256 IS DISTINCT FROM digest OR d.bytes IS DISTINCT FROM a.bytes
  OR d.bucket IS DISTINCT FROM a.bucket OR d.object_version IS DISTINCT FROM a.object_version THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_transport_documents_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_transport_documents_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Resource/profile shape gate only. Live qualification and measured runtime
-- installation are separate checks required before child dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_profile_bound(p_params jsonb,p_profile jsonb,p_image jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE estimate jsonb;k text;w bigint;h bigint;pixels bigint;
BEGIN
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' OR jsonb_typeof(p_image) IS DISTINCT FROM 'object'
 OR p_image->>'dtype' IS DISTINCT FROM 'float32'
 OR p_image->'pixels' IS DISTINCT FROM p_params->'pixels'
 OR p_image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR p_image->'color'->>'encoding' IS DISTINCT FROM 'gamma24'
 OR jsonb_typeof(p_image->'canvas'->'width') IS DISTINCT FROM 'number'
 OR jsonb_typeof(p_image->'canvas'->'height') IS DISTINCT FROM 'number'
 OR coalesce(p_image->'canvas'->>'width','') !~ '^[1-9][0-9]{0,8}$'
 OR coalesce(p_image->'canvas'->>'height','') !~ '^[1-9][0-9]{0,8}$' THEN RETURN false;END IF;
 w:=(p_image->'canvas'->>'width')::bigint;h:=(p_image->'canvas'->>'height')::bigint;
 pixels:=w*h;
 IF w>200000000 OR h>200000000 OR pixels NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object'
 OR NOT(p_profile ?& ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification'])
 OR (p_profile-ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification'])<>'{}'::jsonb
 OR p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR jsonb_typeof(p_profile->'qualification') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_profile->'dataset_sha') IS DISTINCT FROM 'string'
 OR coalesce(p_profile->>'dataset_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
 IF jsonb_typeof(p_profile->k) IS DISTINCT FROM 'string' OR coalesce(p_profile->>k,'') !~ '^[0-9a-f]{64}$'
 OR p_profile->k IS DISTINCT FROM p_params->k THEN RETURN false;END IF;
 END LOOP;
 estimate:=p_profile->'resource_estimate';
 IF jsonb_typeof(estimate) IS DISTINCT FROM 'object'
 OR NOT(estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate-ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'] LOOP
 IF jsonb_typeof(estimate->k) IS DISTINCT FROM 'number' OR coalesce(estimate->>k,'') !~ '^(0|[1-9][0-9]{0,15})$'
 OR (estimate->>k)::numeric>9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 RETURN (estimate->>'pixel_count')::bigint=pixels AND (estimate->>'vram_bytes')::bigint=0
 AND (estimate->>'ram_bytes')::bigint>=67108864+1
 AND (estimate->>'scratch_bytes')::bigint>=24*pixels+32768+16777216
 AND (estimate->>'output_bytes')::bigint>=12*pixels+16384+1048576;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_profile_bound(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_profile_bound(jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Exact semantic edit content binding only. The caller must prove artifact
-- ownership and inclusion in the authenticated committed decision batch.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_approved_edit_bound(p_ref jsonb,p_body text,
 p_preview_params jsonb,p_candidate jsonb,p_intent jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_ref) IS DISTINCT FROM 'object' OR NOT(p_ref ?& ARRAY['artifact_id','sha256'])
 OR (p_ref-'artifact_id'-'sha256')<>'{}'::jsonb
 OR jsonb_typeof(p_ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(p_ref->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(p_ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_ref->>'artifact_id'='00000000-0000-0000-0000-000000000000'
 OR coalesce(p_ref->>'sha256','') !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_ref->>'sha256'
 OR jsonb_typeof(p_preview_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_candidate) IS DISTINCT FROM 'object' OR jsonb_typeof(p_intent) IS DISTINCT FROM 'object'
 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 IF jsonb_typeof(e) IS DISTINCT FROM 'object'
 OR NOT(e ?& ARRAY['schema_version','target_id','source','intent','candidate','mask','transform','kind','strength','ev','prompt','preserve_persons'])
 OR (e-ARRAY['schema_version','target_id','source','intent','candidate','mask','transform','kind','strength','ev','prompt','preserve_persons'])<>'{}'::jsonb
 OR e->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR e->'target_id' IS DISTINCT FROM p_preview_params->'target_id'
 OR e->'source' IS DISTINCT FROM p_preview_params->'source'
 OR e->'intent' IS DISTINCT FROM p_preview_params->'intent'
 OR e->'candidate' IS DISTINCT FROM p_preview_params->'candidate'
 OR e->'mask' IS DISTINCT FROM p_candidate->'mask'
 OR e->'transform' IS DISTINCT FROM p_candidate->'transform'
 OR jsonb_typeof(e->'mask') IS DISTINCT FROM 'object' OR jsonb_typeof(e->'transform') IS DISTINCT FROM 'object'
 OR e->>'kind' IS DISTINCT FROM 'background_cleanup' OR p_intent->>'kind' IS DISTINCT FROM 'background_cleanup'
 OR e->'preserve_persons' IS DISTINCT FROM 'true'::jsonb OR p_intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
 OR e->'ev' IS DISTINCT FROM 'null'::jsonb OR jsonb_typeof(e->'strength') IS DISTINCT FROM 'number'
 OR coalesce(p_intent->'ev','null'::jsonb) IS DISTINCT FROM 'null'::jsonb OR e->'strength' IS DISTINCT FROM p_intent->'strength'
 OR e->'prompt' IS DISTINCT FROM coalesce(p_intent->'prompt','null'::jsonb) THEN RETURN false;END IF;
 RETURN (e->>'strength')::numeric BETWEEN 0 AND 1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_approved_edit_bound(jsonb,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_approved_edit_bound(jsonb,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Locked content/transport preflight only. Decision-batch membership, publication
-- run authority, quota and relational graph materialization remain mandatory in
-- the caller's transaction. This helper creates no runnable task or approval.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_background_cleanup_admission_documents_ready(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
 p_child uuid,p_target uuid,p_document_run uuid,p_revision bigint,p_target_revision bigint,
 p_body text,p_sha text,p_documents jsonb,p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;preview jsonb;selection record;k text;raw text;item record;a photo.artifacts%ROWTYPE;
 presentation jsonb;numerical jsonb;image jsonb;source jsonb;candidate jsonb;intent jsonb;project uuid;
BEGIN
 IF jsonb_typeof(p_documents) IS DISTINCT FROM 'object'
 OR NOT(p_documents ?& ARRAY['approved','presentation','numerical','image','candidate','intent','source','reference','mask','protection'])
 OR (p_documents-ARRAY['approved','presentation','numerical','image','candidate','intent','source','reference','mask','protection'])<>'{}'::jsonb
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['approved','presentation','numerical','image','candidate','intent','source','reference','mask','protection'] LOOP
 IF jsonb_typeof(p_documents->k) IS DISTINCT FROM 'string' OR octet_length(p_documents->>k) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 END LOOP;
 e:=p_body::jsonb;presentation:=(p_documents->>'presentation')::jsonb;
 preview:=presentation->'params';candidate:=(p_documents->>'candidate')::jsonb;intent:=(p_documents->>'intent')::jsonb;
 numerical:=(p_documents->>'numerical')::jsonb;image:=(p_documents->>'image')::jsonb;source:=(p_documents->>'source')::jsonb;
 SELECT * INTO selection FROM photo.lock_semantic_shown_background_cleanup_params(p_tenant,p_intent,p_actor,p_session,p_child,p_target,
 p_revision,p_target_revision,preview->'plan'->>'sha256',preview->'candidate'->>'sha256',p_body,p_sha,e->'approved_edit');
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=selection.preview_id
 AND r.config_snapshot->'semantic_background_cleanup_preview'->'params'=preview) THEN RETURN false;END IF;
 FOR item IN SELECT * FROM (VALUES
 ('approved',e->'approved_edit'),('presentation',e->'presentation'),('numerical',e->'numerical_evidence'),
 ('image',e->'working_image'),('candidate',preview->'candidate'),('intent',preview->'intent'),
 ('source',preview->'source'->'source'),('reference',intent->'reference'->'source'),
 ('mask',candidate->'mask'),('protection',preview->'protection')) AS bindings(name,ref) LOOP
 raw:=p_documents->>item.name;
 IF encode(public.digest(convert_to(raw,'UTF8'),'sha256'),'hex') IS DISTINCT FROM item.ref->>'sha256' THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(item.ref->>'artifact_id')::uuid;
 IF NOT FOUND OR a.sha256 IS DISTINCT FROM item.ref->>'sha256' OR a.bytes IS DISTINCT FROM octet_length(raw)::bigint THEN RETURN false;END IF;
 END LOOP;
 IF numerical->'params' IS DISTINCT FROM preview OR numerical->'working_image' IS DISTINCT FROM e->'working_image'
 OR numerical->'pixels' IS DISTINCT FROM e->'pixels' OR numerical->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR numerical->>'algorithm' IS DISTINCT FROM 'big_lama_work_srgb_delta_v1'
 OR numerical->'protected_pixels_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR numerical->'outside_mask_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR jsonb_typeof(numerical->'global_pass') IS DISTINCT FROM 'boolean'
 OR jsonb_typeof(numerical->'changed_pixels') IS DISTINCT FROM 'number'
 OR jsonb_typeof(numerical->'roi') IS DISTINCT FROM 'array'
 OR image->'pixels' IS DISTINCT FROM e->'pixels' OR (image-'pixels') IS DISTINCT FROM (source-'pixels')
 OR p_node->>'params_sha' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 IF photo.semantic_shown_background_cleanup_approved_edit_bound(e->'approved_edit',p_documents->>'approved',preview,candidate,intent) IS DISTINCT FROM true
 OR photo.semantic_shown_background_cleanup_inventory_exact(e,presentation,preview,p_node,p_inventory,candidate,intent,source,
 (p_documents->>'reference')::jsonb,(p_documents->>'mask')::jsonb,(p_documents->>'protection')::jsonb) IS DISTINCT FROM true
 OR photo.semantic_shown_background_cleanup_profile_bound(e,p_node->'execution_profile',image) IS DISTINCT FROM true
 OR photo.semantic_shown_background_cleanup_transport_documents_ready(p_tenant,p_document_run,p_child,e,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 RETURN photo.v27_downstream_profile_ready(p_tenant,project,'semantic.adopt_shown_background_cleanup','1','cpu',p_node->'execution_profile') IS NOT DISTINCT FROM true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_background_cleanup_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_background_cleanup_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_background_cleanup_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)
 TO photo_semantic_admission;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
