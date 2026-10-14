-- +goose Up
-- No metadata task can consume a source omitted from pinned parent inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_inventory_child_valid(p_inventory jsonb,p_config jsonb,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;entry jsonb;ordinal integer;names text[];
BEGIN
 request:=p_config->'v30_delivery_export';ordinal:=(request->>'ordinal')::integer;
 IF ordinal IS NULL OR ordinal NOT BETWEEN 0 AND 4999 OR p_approved IS NULL
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object' OR p_inventory->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_inventory->>'parent_run_id' IS DISTINCT FROM request->>'parent_run_id'
 OR p_inventory->>'snapshot_digest' IS DISTINCT FROM request->>'snapshot_digest'
 OR p_inventory->>'policy' IS DISTINCT FROM ((request->>'config_jcs')::jsonb)->>'metadata_policy'
 OR jsonb_typeof(p_inventory->'items') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 IF p_inventory->'approved_fields' IS DISTINCT FROM to_jsonb(names) THEN RETURN false;END IF;
 entry:=p_inventory->'items'->ordinal;
 RETURN entry IS NOT NULL AND entry->'ordinal'=to_jsonb(ordinal)
 AND entry->'binding' IS NOT DISTINCT FROM request->'binding'
 AND entry->'metadata_source' IS NOT DISTINCT FROM p_config->'v30_metadata_source'
 AND entry->>'publication_sha256' IS NOT DISTINCT FROM p_config->>'v30_metadata_publication_sha256'
 AND entry->>'frozen_sha256' IS NOT DISTINCT FROM p_config->>'v30_metadata_frozen_sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_inventory_child_valid(jsonb,jsonb,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_inventory_child_valid(jsonb,jsonb,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$ BEGIN IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[])'::regprocedure) IS DISTINCT FROM '4a87ef51dff22c06d23d3469b32c4883' THEN RAISE EXCEPTION 'METADATA_INVENTORY_CHILD_DEFINITION_DRIFT';END IF;END $$;
CREATE OR REPLACE FUNCTION photo.v30_metadata_child_source_ready(p_tenant uuid,p_child uuid,p_config jsonb,p_proof jsonb,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;parent uuid;publication_sha text;project uuid;names text[];frozen jsonb;inventory_raw text;inventory_sha text;
BEGIN
 IF jsonb_typeof(p_config) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_config))<>8
 OR p_config->>'queue_class' IS DISTINCT FROM 'batch'
 OR p_config->'v30_metadata_source' IS DISTINCT FROM p_proof->'metadata_source'
 OR p_config->>'v30_metadata_frozen_sha256' IS NULL
 OR p_config->>'v30_metadata_frozen_sha256' IS DISTINCT FROM p_proof->>'frozen_sha256'
 OR coalesce(p_config->>'v30_metadata_publication_sha256','') !~ '^[0-9a-f]{64}$'
 OR p_approved IS NULL THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 IF p_config->'v30_metadata_approved_fields' IS DISTINCT FROM to_jsonb(names) THEN RETURN false;END IF;
 request:=p_config->'v30_delivery_export';parent:=(request->>'parent_run_id')::uuid;project:=(p_config->>'project_id')::uuid;
 IF request->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR request->>'child_run_id' IS DISTINCT FROM p_child::text
 OR request->'binding' IS DISTINCT FROM p_proof->'binding'
 OR ((request->>'config_jcs')::jsonb)->>'metadata_policy' IS DISTINCT FROM p_proof->>'policy'
 OR (p_config->>'export_id')::uuid IS NULL OR (p_config->>'export_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR parent IS NULL OR project IS NULL THEN RETURN false;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b JOIN photo.tasks prepare
 ON prepare.tenant_id=b.tenant_id AND prepare.id=b.owner_id AND prepare.run_id=parent
 WHERE b.tenant_id=p_tenant AND b.run_id=parent AND b.purpose='task_params' AND b.state='ready'
 AND prepare.operation='v2.delivery.prepare' AND b.artifact_id=prepare.params_artifact_id;
 IF photo.v30_metadata_policy_approval_valid(frozen->'policy',p_proof->>'policy',p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT r.v30_metadata_inventory_jcs,r.v30_metadata_inventory_sha INTO inventory_raw,inventory_sha
 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=parent;
 IF inventory_raw IS NULL OR inventory_sha IS DISTINCT FROM encode(sha256(convert_to(inventory_raw,'UTF8')),'hex')
 OR photo.v30_metadata_inventory_child_valid(inventory_raw::jsonb,p_config,p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v30_metadata_published_source_ready(p_tenant,parent,p_proof,p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT d.v30_metadata_source_sha INTO publication_sha FROM photo.backend_documents d
 WHERE d.tenant_id=p_tenant AND d.run_id=parent AND d.purpose='v30_metadata_source' AND d.state='ready'
 AND d.artifact_id=(p_proof->'metadata_source'->>'artifact_id')::uuid
 AND (d.v30_metadata_source_jcs::jsonb)->'scope'->>'project_id'=project::text;
 RETURN publication_sha IS NOT NULL AND publication_sha=p_config->>'v30_metadata_publication_sha256';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_child_source_ready(uuid,uuid,jsonb,jsonb,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
