-- +goose Up
-- All existing child admission/recovery fences share this config constructor.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v32_delivery_child_config(p_tenant uuid,p_project uuid,p_request jsonb,p_export uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;base jsonb;mode text;raw text;pin text;inventory jsonb;entry jsonb;approved text[];publication record;proof jsonb;
BEGIN
 IF p_request IS NULL OR p_project IS NULL OR p_export IS NULL THEN RETURN NULL;END IF;
 base:=jsonb_build_object('project_id',p_project,'v30_delivery_export',p_request,'export_id',p_export);
 mode:=((p_request->>'config_jcs')::jsonb)->>'metadata_policy';
 IF mode IS NULL OR mode NOT IN('strip','copyright','approved_exif') THEN RETURN NULL;END IF;
 SELECT config_snapshot INTO saved FROM photo.runs WHERE tenant_id=p_tenant AND id=(p_request->>'child_run_id')::uuid;
 IF mode='strip' THEN
  IF FOUND AND NOT(saved ? 'queue_class') THEN RETURN base;END IF;
  IF FOUND AND saved->'queue_class' IS DISTINCT FROM '"batch"'::jsonb THEN RETURN NULL;END IF;
  RETURN base||jsonb_build_object('queue_class','batch');
 END IF;
 SELECT r.v30_metadata_inventory_jcs,r.v30_metadata_inventory_sha INTO raw,pin
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=(p_request->>'parent_run_id')::uuid AND r.kind='v2'
 AND r.status='running' AND NOT r.cancel_requested AND w.project_id=p_project AND w.workflow_type='delivery';
 IF raw IS NULL OR pin IS DISTINCT FROM encode(sha256(convert_to(raw,'UTF8')),'hex') THEN RETURN NULL;END IF;
 inventory:=raw::jsonb;entry:=inventory->'items'->((p_request->>'ordinal')::integer);
 IF entry IS NULL OR jsonb_typeof(inventory->'approved_fields') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved FROM jsonb_array_elements_text(inventory->'approved_fields');
 base:=base||jsonb_build_object('queue_class','batch','v30_metadata_source',entry->'metadata_source',
 'v30_metadata_approved_fields',to_jsonb(approved),'v30_metadata_publication_sha256',entry->>'publication_sha256','v30_metadata_frozen_sha256',entry->>'frozen_sha256');
 IF photo.v30_metadata_inventory_child_valid(inventory,base,approved) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO publication FROM photo.read_v30_metadata_source(p_tenant,(p_request->>'parent_run_id')::uuid,p_request->'binding',entry->'metadata_source',mode,approved);
 IF NOT FOUND OR publication.publication_sha IS DISTINCT FROM entry->>'publication_sha256' THEN RETURN NULL;END IF;
 proof:=publication.publication_jcs::jsonb;
 IF proof->'scope'->>'project_id' IS DISTINCT FROM p_project::text OR proof->>'frozen_sha256' IS DISTINCT FROM entry->>'frozen_sha256' THEN RETURN NULL;END IF;
 IF saved IS NOT NULL AND saved IS DISTINCT FROM base THEN RETURN NULL;END IF;
 RETURN base;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v32_delivery_child_config(uuid,uuid,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_delivery_child_config(uuid,uuid,jsonb,uuid) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.create_v30_delivery_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)'::regprocedure);
 needle:='(cfg-''naming_template''-''watermark''-''sharpen'')||jsonb_build_object(''image'',expected->''binding''->''source'',''icc_sha256'',params->>''icc_sha256'')';
 replacement:='(cfg-''naming_template''-''watermark''-''sharpen'')||jsonb_build_object(''image'',expected->''binding''->''source'',''icc_sha256'',params->>''icc_sha256'',''metadata_policy'',''strip'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'METADATA_CHILD_ENCODE_DEFINITION_DRIFT';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='jsonb_build_object(''metadata_policy'',''strip'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>2 THEN RAISE EXCEPTION 'METADATA_CHILD_EXPORT_POLICY_DRIFT';END IF;
 EXECUTE replace(definition,needle,'jsonb_build_object(''metadata_policy'',cfg->>''metadata_policy'')');
 definition:=pg_get_functiondef('photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'METADATA_CHILD_CURSOR_POLICY_DRIFT';END IF;
 EXECUTE replace(definition,needle,'jsonb_build_object(''metadata_policy'',((cfg->>''config_jcs'')::jsonb)->>''metadata_policy'')');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
