-- +goose Up
-- Locked content/transport preflight only. Decision-batch membership, publication
-- run authority, quota and relational graph materialization remain mandatory in
-- the caller's transaction. This helper creates no runnable task or approval.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_admission_documents_ready(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,
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
 SELECT * INTO selection FROM photo.lock_semantic_shown_exposure_params(p_tenant,p_intent,p_actor,p_session,p_child,p_target,
 p_revision,p_target_revision,preview->'plan'->>'sha256',preview->'candidate'->>'sha256',p_body,p_sha,e->'approved_edit');
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=selection.preview_id
 AND r.config_snapshot->'semantic_preapply_preview'->'params'=preview) THEN RETURN false;END IF;
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
 OR numerical->>'algorithm' IS DISTINCT FROM 'local_exposure_exact_replay_v1'
 OR numerical->'protected_pixels_unchanged' IS DISTINCT FROM 'true'::jsonb
 OR numerical->'outside_mask_max_error' IS DISTINCT FROM '0'::jsonb
 OR image->'pixels' IS DISTINCT FROM e->'pixels' OR (image-'pixels') IS DISTINCT FROM (source-'pixels')
 OR p_node->>'params_sha' IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 IF photo.semantic_shown_exposure_approved_edit_bound(e->'approved_edit',p_documents->>'approved',preview,candidate,intent) IS DISTINCT FROM true
 OR photo.semantic_shown_exposure_inventory_exact(e,presentation,preview,p_node,p_inventory,candidate,intent,source,
 (p_documents->>'reference')::jsonb,(p_documents->>'mask')::jsonb,(p_documents->>'protection')::jsonb) IS DISTINCT FROM true
 OR photo.semantic_shown_exposure_profile_bound(e,p_node->'execution_profile',image) IS DISTINCT FROM true
 OR photo.semantic_shown_exposure_transport_documents_ready(p_tenant,p_document_run,p_child,e,p_node,p_transport,p_inventory) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT project_id INTO project FROM photo.semantic_intents WHERE tenant_id=p_tenant AND id=p_intent;
 RETURN photo.v27_downstream_profile_ready(p_tenant,project,'semantic.adopt_shown_exposure','1','cpu',p_node->'execution_profile') IS NOT DISTINCT FROM true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_exposure_admission_documents_ready(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text,jsonb,jsonb,jsonb,jsonb)
 TO photo_semantic_admission;
-- +goose StatementEnd
