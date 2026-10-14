-- +goose Up
-- Recover the complete retained typed input inventory under current source authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_assembly_inventory(p_expansion uuid,p_token uuid,p_root uuid,p_sha text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE raw text;body jsonb;e photo.graph_expansions%ROWTYPE;owner uuid;entries jsonb;total integer;locked integer;
BEGIN
 raw:=photo.v30_delivery_saved_assembly(p_expansion,p_token,p_root,p_sha);
 IF raw IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 owner:=photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0');body:=raw::jsonb;
 WITH all_entries AS (
 SELECT p_root artifact_id,p_sha sha256,'DeliveryAssemblyInput'::text contract
 UNION ALL SELECT (value->>'artifact_id')::uuid,value->>'sha256','DeliverySlotShard' FROM jsonb_array_elements(body->'shards')
 UNION ALL SELECT (value->'config'->>'artifact_id')::uuid,value->'config'->>'sha256','ExportPresetConfig' FROM jsonb_array_elements(body->'presets')
 UNION ALL SELECT (item->'export'->>'artifact_id')::uuid,item->'export'->>'sha256','EncodedExport'
 FROM jsonb_array_elements(body->'shards') shard JOIN photo.backend_documents d
 ON d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=owner AND d.state='ready'
 AND d.artifact_id=(shard->>'artifact_id')::uuid AND d.sha256=shard->>'sha256'
 CROSS JOIN LATERAL jsonb_array_elements(d.v30_delivery_document_jcs::jsonb->'items') item WHERE item->>'state'='ready'
 UNION ALL SELECT artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=e.parent_task_id
 AND contract NOT IN('Params','InputClosureChunk','InputClosureManifest')
 ), unique_entries AS (SELECT DISTINCT artifact_id,sha256,contract FROM all_entries)
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',artifact_id,'sha256',sha256),'contract',contract) ORDER BY artifact_id) INTO entries FROM unique_entries;
 total:=jsonb_array_length(entries);
 -- Leave one inventory entry for the new assemble Params document.
 IF total IS NULL OR total NOT BETWEEN 3 AND 999999 OR EXISTS(
 SELECT 1 FROM jsonb_array_elements(entries) x GROUP BY x->'ref'->>'artifact_id' HAVING count(*)<>1) THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(entries) x WHERE NOT EXISTS(
 SELECT 1 FROM photo.v2_artifact_links l JOIN photo.v2_workflows w ON w.tenant_id=l.tenant_id AND w.id=l.workflow_id
 WHERE l.tenant_id=e.tenant_id AND w.run_id=e.run_id AND l.artifact_id=(x->'ref'->>'artifact_id')::uuid AND l.sha256=x->'ref'->>'sha256')) THEN RETURN NULL;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN jsonb_array_elements(entries) x
 ON a.id=(x->'ref'->>'artifact_id')::uuid AND a.sha256=x->'ref'->>'sha256'
 WHERE a.tenant_id=e.tenant_id AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.id FOR SHARE OF a;
 GET DIAGNOSTICS locked=ROW_COUNT;
 IF locked<>total OR NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=e.id AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN entries;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_inventory(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_inventory(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_assembly_inventory(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
