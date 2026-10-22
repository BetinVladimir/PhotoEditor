-- +goose Up
-- Private proposed-target image/dependency provenance, without Apply approval.
-- Physical arrays, ICC and geometry require independent worker verification.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_preapply_preview_images_ready(p_tenant uuid,p_intent uuid,
 p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,p_body text,p_sha text,
 p_intent_body text,p_source_body text,p_reference_body text,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;intent jsonb;image jsonb;pin jsonb;raw text;root record;image_binary record;child record;is_reference boolean;
BEGIN
 PERFORM photo.lock_semantic_preapply_preview_publication(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,p_body,p_sha);
 e:=p_body::jsonb;
 IF p_intent_body IS NULL OR octet_length(p_intent_body) NOT BETWEEN 2 AND 1048576
 OR p_source_body IS NULL OR octet_length(p_source_body) NOT BETWEEN 2 AND 1048576
 OR p_reference_body IS NULL OR octet_length(p_reference_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_inventory->'entries') IS DISTINCT FROM 'array'
 OR encode(public.digest(convert_to(p_intent_body,'UTF8'),'sha256'),'hex') IS DISTINCT FROM e->'intent'->>'sha256'
 THEN RETURN false;END IF;
 intent:=p_intent_body::jsonb;
 IF intent->>'kind' IS DISTINCT FROM 'local_exposure' OR intent->'preserve_persons' IS DISTINCT FROM 'true'::jsonb
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
ALTER FUNCTION photo.semantic_preapply_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_preapply_preview_images_ready(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
