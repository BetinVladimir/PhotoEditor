-- +goose Up
-- Prepare rechecks each original under current API authority and workflow project.
-- +goose StatementBegin
ALTER FUNCTION photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text)
 RENAME TO v30_metadata_parent_inventory_ready_before_live_sources;
REVOKE ALL ON FUNCTION photo.v30_metadata_parent_inventory_ready_before_live_sources(uuid,uuid,uuid,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.v30_metadata_parent_inventory_ready(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_frozen jsonb,p_snapshot text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;inventory jsonb;entry jsonb;envelope jsonb;document photo.backend_documents%ROWTYPE;approved text[];
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v30_metadata_parent_inventory_ready_before_live_sources(p_tenant,p_run,p_actor,p_session,p_frozen,p_snapshot) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT w.project_id,r.v30_metadata_inventory_jcs::jsonb INTO project,inventory
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.created_by=p_actor
 AND w.workflow_type='delivery' AND w.renderer_version='v2.2';
 IF NOT FOUND OR project IS NULL THEN RETURN false;END IF;
 SELECT coalesce(array_agg(value ORDER BY value COLLATE "C"),ARRAY[]::text[]) INTO approved
 FROM jsonb_array_elements_text(inventory->'approved_fields');
 FOR entry IN SELECT value FROM jsonb_array_elements(inventory->'items') LOOP
  SELECT * INTO document FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
  AND purpose='v30_metadata_source' AND state='ready' AND artifact_id=(entry->'metadata_source'->>'artifact_id')::uuid;
  IF NOT FOUND THEN RETURN false;END IF;
  envelope:=document.v30_metadata_source_jcs::jsonb;
  IF photo.v30_metadata_source_authorized(p_tenant,project,p_actor,p_session,envelope->'scope',
   document.v30_metadata_source_jcs,document.v30_metadata_source_sha,entry->'metadata_source',
   envelope->>'metadata_jcs',inventory->>'policy',approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
