-- +goose Up
-- Inventory is run metadata, not a second object-store document or quota charge.
ALTER TABLE photo.runs ADD COLUMN v30_metadata_inventory_jcs text;
ALTER TABLE photo.runs ADD COLUMN v30_metadata_inventory_sha text;
ALTER TABLE photo.runs ADD CONSTRAINT v30_metadata_inventory_pair CHECK(
 (v30_metadata_inventory_jcs IS NULL AND v30_metadata_inventory_sha IS NULL) OR
 (v30_metadata_inventory_jcs IS NOT NULL AND v30_metadata_inventory_sha IS NOT NULL
 AND octet_length(v30_metadata_inventory_jcs) BETWEEN 2 AND 8388608
 AND v30_metadata_inventory_sha=encode(sha256(convert_to(v30_metadata_inventory_jcs,'UTF8')),'hex')));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v30_metadata_inventory() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
 IF NEW.v30_metadata_inventory_jcs IS NOT NULL OR NEW.v30_metadata_inventory_sha IS NOT NULL THEN
 RAISE EXCEPTION 'METADATA_INVENTORY_REQUIRES_EXISTING_RUN' USING ERRCODE='42501';END IF;
 RETURN NEW;END IF;
 IF current_user<>'photo_fn_owner' AND
 (NEW.v30_metadata_inventory_jcs IS DISTINCT FROM OLD.v30_metadata_inventory_jcs OR NEW.v30_metadata_inventory_sha IS DISTINCT FROM OLD.v30_metadata_inventory_sha) THEN
 RAISE EXCEPTION 'METADATA_INVENTORY_REQUIRES_TRUSTED_PIN' USING ERRCODE='42501';END IF;
 IF OLD.v30_metadata_inventory_jcs IS NOT NULL AND
 (NEW.v30_metadata_inventory_jcs IS DISTINCT FROM OLD.v30_metadata_inventory_jcs OR NEW.v30_metadata_inventory_sha IS DISTINCT FROM OLD.v30_metadata_inventory_sha) THEN
 RAISE EXCEPTION 'IMMUTABLE_METADATA_INVENTORY' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_metadata_inventory() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_metadata_inventory() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE TRIGGER immutable_v30_metadata_inventory BEFORE INSERT OR UPDATE OF v30_metadata_inventory_jcs,v30_metadata_inventory_sha ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_metadata_inventory();
CREATE FUNCTION photo.pin_v30_metadata_inventory(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_raw text,p_sha text,p_snapshot text,p_key text,p_request_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;inventory jsonb;snapshot jsonb;frozen jsonb;
 item jsonb;source_item jsonb;document photo.backend_documents%ROWTYPE;envelope jsonb;approved text[];position integer:=0;saved jsonb;frozen_doc jsonb;snapshot_doc jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 8388608
 OR p_snapshot IS NULL OR octet_length(p_snapshot) NOT BETWEEN 2 AND 8388608
 OR p_sha IS NULL OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_raw,'UTF8')),'hex') THEN RETURN false;END IF;
 inventory:=p_raw::jsonb;snapshot:=p_snapshot::jsonb;
 IF jsonb_typeof(inventory) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(inventory))<>8
 OR inventory->'schema_version' IS DISTINCT FROM '1'::jsonb OR inventory->>'parent_run_id' IS DISTINCT FROM p_run::text
 OR inventory->>'policy' IS NULL OR inventory->>'policy' NOT IN('copyright','approved_exif')
 OR jsonb_typeof(inventory->'approved_fields') IS DISTINCT FROM 'array'
 OR jsonb_typeof(inventory->'items') IS DISTINCT FROM 'array'
 OR jsonb_typeof(snapshot->'items') IS DISTINCT FROM 'array'
 OR jsonb_array_length(snapshot->'items') NOT BETWEEN 1 AND 5000
 OR jsonb_array_length(inventory->'items')<>jsonb_array_length(snapshot->'items')
 OR inventory->>'snapshot_digest' IS DISTINCT FROM snapshot->>'snapshot_digest'
 OR inventory->'snapshot'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_snapshot,'UTF8')),'hex') THEN RETURN false;END IF;
 SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved FROM jsonb_array_elements_text(inventory->'approved_fields');
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR SHARE;
 IF saved IS NULL OR p_run<>photo.v30_uuid5(p_tenant,'delivery:create:'||p_actor||':'||p_key) THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF NOT FOUND OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2' THEN RETURN false;END IF;
 SELECT value INTO frozen_doc FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_input';
 SELECT value INTO snapshot_doc FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='v30_delivery_snapshot';
 frozen:=(frozen_doc->>'body_jcs')::jsonb;
 IF frozen IS NULL OR frozen->'snapshot' IS DISTINCT FROM inventory->'snapshot'
 OR snapshot_doc->>'body_jcs' IS DISTINCT FROM p_snapshot
 OR photo.v30_metadata_policy_approval_valid(frozen->'policy',inventory->>'policy',approved) IS DISTINCT FROM true
 OR photo.v30_delivery_create_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
 (frozen_doc->'ref'->>'artifact_id')::uuid,'v30_delivery_input',frozen_doc->>'body_jcs') IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
 AND b.purpose='v30_delivery_input' AND b.state='ready' AND b.artifact_id=(frozen_doc->'ref'->>'artifact_id')::uuid
 AND b.sha256=frozen_doc->'ref'->>'sha256')
 OR NOT EXISTS(SELECT 1 FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run
 AND b.purpose='v30_delivery_snapshot' AND b.state='ready' AND b.artifact_id=(inventory->'snapshot'->>'artifact_id')::uuid
 AND b.sha256=inventory->'snapshot'->>'sha256') THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(inventory->'items') LOOP
  source_item:=snapshot->'items'->position;
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(item))<>5
  OR item->'ordinal' IS DISTINCT FROM to_jsonb(position) OR item->'binding' IS DISTINCT FROM source_item->'binding' THEN RETURN false;END IF;
  SELECT * INTO document FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
  AND purpose='v30_metadata_source' AND state='ready' AND artifact_id=(item->'metadata_source'->>'artifact_id')::uuid;
  IF NOT FOUND OR document.sha256 IS DISTINCT FROM item->'metadata_source'->>'sha256'
  OR document.v30_metadata_source_sha IS DISTINCT FROM item->>'publication_sha256' THEN RETURN false;END IF;
  envelope:=document.v30_metadata_source_jcs::jsonb;
  IF envelope->'scope'->'binding' IS DISTINCT FROM item->'binding'
  OR envelope->>'frozen_sha256' IS DISTINCT FROM item->>'frozen_sha256'
  OR photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,document.v30_metadata_source_jcs,
  document.v30_metadata_source_sha,inventory->>'policy',approved) IS NULL THEN RETURN false;END IF;
  position:=position+1;
 END LOOP;
 IF r.v30_metadata_inventory_jcs IS NOT NULL THEN
 RETURN r.v30_metadata_inventory_jcs=p_raw AND r.v30_metadata_inventory_sha=p_sha;END IF;
 UPDATE photo.runs SET v30_metadata_inventory_jcs=p_raw,v30_metadata_inventory_sha=p_sha WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.pin_v30_metadata_inventory(uuid,uuid,uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pin_v30_metadata_inventory(uuid,uuid,uuid,uuid,text,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.pin_v30_metadata_inventory(uuid,uuid,uuid,uuid,text,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
