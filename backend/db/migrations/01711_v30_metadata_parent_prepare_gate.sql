-- +goose Up
-- Parent metadata inventory is mandatory before prepare source admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_parent_inventory_ready(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_frozen jsonb,p_snapshot text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE raw text;pin text;inventory jsonb;snapshot jsonb;entry jsonb;source_item jsonb;
 document photo.backend_documents%ROWTYPE;envelope jsonb;approved text[];ordinal integer:=0;
BEGIN
 SELECT r.v30_metadata_inventory_jcs,r.v30_metadata_inventory_sha INTO raw,pin FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run;
 IF raw IS NULL OR pin IS DISTINCT FROM encode(sha256(convert_to(raw,'UTF8')),'hex') OR p_snapshot IS NULL THEN RETURN false;END IF;
 inventory:=raw::jsonb;snapshot:=p_snapshot::jsonb;
 IF inventory->>'parent_run_id' IS DISTINCT FROM p_run::text OR inventory->'snapshot' IS DISTINCT FROM p_frozen->'snapshot'
 OR inventory->>'snapshot_digest' IS DISTINCT FROM p_frozen->>'snapshot_digest'
 OR inventory->'snapshot'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_snapshot,'UTF8')),'hex')
 OR jsonb_typeof(inventory->'items') IS DISTINCT FROM 'array' OR jsonb_typeof(snapshot->'items') IS DISTINCT FROM 'array'
 OR jsonb_typeof(inventory->'approved_fields') IS DISTINCT FROM 'array'
 OR jsonb_array_length(inventory->'items')<>jsonb_array_length(snapshot->'items') THEN RETURN false;END IF;
 SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved FROM jsonb_array_elements_text(inventory->'approved_fields');
 IF photo.v30_metadata_policy_approval_valid(p_frozen->'policy',inventory->>'policy',approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(inventory->'items') LOOP
  source_item:=snapshot->'items'->ordinal;
  IF entry->'ordinal' IS DISTINCT FROM to_jsonb(ordinal) OR entry->'binding' IS DISTINCT FROM source_item->'binding' THEN RETURN false;END IF;
  SELECT * INTO document FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND purpose='v30_metadata_source'
  AND state='ready' AND artifact_id=(entry->'metadata_source'->>'artifact_id')::uuid;
  IF NOT FOUND OR document.sha256 IS DISTINCT FROM entry->'metadata_source'->>'sha256'
  OR document.v30_metadata_source_sha IS DISTINCT FROM entry->>'publication_sha256' THEN RETURN false;END IF;
  envelope:=document.v30_metadata_source_jcs::jsonb;
  IF envelope->'scope'->>'tenant_id' IS DISTINCT FROM p_tenant::text OR envelope->'scope'->>'actor_id' IS DISTINCT FROM p_actor::text
  OR envelope->'scope'->>'session_id' IS DISTINCT FROM p_session::text OR envelope->'scope'->'binding' IS DISTINCT FROM entry->'binding'
  OR envelope->>'frozen_sha256' IS DISTINCT FROM entry->>'frozen_sha256'
  OR photo.v30_metadata_source_publication_valid(document.v30_metadata_source_jcs,document.v30_metadata_source_sha,envelope->'scope',
  entry->'metadata_source',envelope->>'metadata_jcs',inventory->>'policy',approved) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=document.artifact_id
  AND a.sha256=document.sha256 AND a.bytes=document.bytes AND a.bucket=document.bucket AND a.object_version=document.object_version
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='v30_metadata_source'
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  ordinal:=ordinal+1;
 END LOOP;
 RETURN ordinal BETWEEN 1 AND 5000;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_delivery_prepare_source_ready_before_tenant_context(uuid,uuid,uuid,uuid,text,text)'::regprocedure);
 needle:='frozen->''policy''->>''metadata_policy'' IS DISTINCT FROM ''strip''';
 replacement:='(frozen->''policy''->>''metadata_policy'' IS NULL OR frozen->''policy''->>''metadata_policy'' NOT IN(''strip'',''copyright'',''approved_exif''))';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'METADATA_PREPARE_POLICY_DEFINITION_DRIFT';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='(raw::jsonb)->>''metadata_policy'' IS DISTINCT FROM ''strip''';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'METADATA_PREPARE_PRESET_DEFINITION_DRIFT';END IF;
 definition:=replace(definition,needle,'(raw::jsonb)->>''metadata_policy'' IS DISTINCT FROM frozen->''policy''->>''metadata_policy''');
 needle:='RETURN photo.v2_editor_authorized(p_tenant,v_project,v_actor,v_session)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'METADATA_PREPARE_INVENTORY_DEFINITION_DRIFT';END IF;
 replacement:='IF frozen->''policy''->>''metadata_policy''<>''strip'' AND photo.v30_metadata_parent_inventory_ready(p_tenant,p_run,v_actor,v_session,frozen,proof->>''snapshot_jcs'') IS DISTINCT FROM true THEN RETURN false;END IF; RETURN photo.v2_editor_authorized(p_tenant,v_project,v_actor,v_session)';
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
