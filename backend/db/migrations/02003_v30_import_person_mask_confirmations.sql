-- +goose Up
-- A confirmation is for one archived recipe/Override/mask occurrence and one
-- newly ingested asset version. Archive flags and person identity are not
-- inherited. The only mutation path will be a separately authorized function.
-- +goose StatementBegin
CREATE TABLE photo.v30_import_person_mask_confirmations(
 tenant_id uuid NOT NULL,
 workflow_id uuid NOT NULL,
 project_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 recipe_id uuid NOT NULL,
 recipe_sha text NOT NULL CHECK(recipe_sha ~ '^[0-9a-f]{64}$'),
 override_id uuid NOT NULL,
 override_sha text NOT NULL CHECK(override_sha ~ '^[0-9a-f]{64}$'),
 mask_id uuid NOT NULL,
 mask_sha text NOT NULL CHECK(mask_sha ~ '^[0-9a-f]{64}$'),
 source_version_id uuid NOT NULL,
 target_version_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('granted','revoked')),
 epoch bigint NOT NULL CHECK(epoch>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,workflow_id,entry_id,override_id,mask_id,target_version_id),
 FOREIGN KEY(tenant_id,workflow_id) REFERENCES photo.v2_workflows(tenant_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,target_version_id) REFERENCES photo.asset_versions(tenant_id,id)
);
ALTER TABLE photo.v30_import_person_mask_confirmations ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_import_person_mask_confirmations FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_import_person_mask_confirmations
 TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.v30_import_person_mask_confirmations
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_import_person_mask_confirmations TO photo_fn_owner;

-- The scheduler may observe a grant only while the complete current import
-- source gate still accepts the parsed document and exact new version.
CREATE FUNCTION photo.v30_import_person_mask_confirmation_live(
 p_tenant uuid,p_workflow uuid,p_entry uuid,p_recipe uuid,p_recipe_sha text,
 p_override uuid,p_override_sha text,p_mask uuid,p_mask_sha text,
 p_source_version uuid,p_target_version uuid,p_parsed text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;confirmation photo.v30_import_person_mask_confirmations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_recipe_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_override_sha,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR NOT EXISTS(
  SELECT 1 FROM jsonb_array_elements(sources->'entries') e
  WHERE e->>'entry_id'=p_entry::text AND e->>'state'='adopted'
  AND e->>'asset_version_id'=p_target_version::text
 ) THEN RETURN NULL;END IF;
 SELECT * INTO confirmation FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow
 AND c.project_id=(sources->>'project_id')::uuid AND c.entry_id=p_entry
 AND c.recipe_id=p_recipe AND c.recipe_sha=p_recipe_sha
 AND c.override_id=p_override AND c.override_sha=p_override_sha
 AND c.mask_id=p_mask AND c.mask_sha=p_mask_sha
 AND c.source_version_id=p_source_version AND c.target_version_id=p_target_version
 AND c.status='granted' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN confirmation.epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_person_mask_confirmation_live(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_confirmation_live(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_mask_confirmation_live(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,uuid,text)
 TO photo_scheduler;

CREATE FUNCTION photo.v30_import_person_mask_prepared_live(
 p_tenant uuid,p_workflow uuid,p_target_version uuid,p_prepared_mask uuid,p_mask_sha text,p_parsed text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;result_epoch bigint;matches integer:=0;candidate record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL THEN RETURN NULL;END IF;
 FOR candidate IN SELECT c.entry_id,c.mask_id,c.epoch FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow
 AND c.project_id=(sources->>'project_id')::uuid AND c.target_version_id=p_target_version
 AND c.mask_sha=p_mask_sha AND c.status='granted' FOR SHARE LOOP
  IF photo.v30_uuid5(p_workflow,'interchange:dependency:'||candidate.entry_id::text||':MaskPixels:'||candidate.mask_id::text)=p_prepared_mask
  AND EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') e
   WHERE e->>'entry_id'=candidate.entry_id::text AND e->>'state'='adopted'
   AND e->>'asset_version_id'=p_target_version::text) THEN
   matches:=matches+1;result_epoch:=candidate.epoch;
  END IF;
 END LOOP;
 IF matches<>1 THEN RETURN NULL;END IF;
 RETURN result_epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;

-- Tenant metadata can be removed only by the already cleanup-gated purge.
DO $$ DECLARE definition text;BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF definition IS NULL OR position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0
 OR position('''v30_import_person_mask_confirmations''' IN definition)>0 THEN
  RAISE EXCEPTION 'V30_IMPORT_MASK_PURGE_ALLOWLIST_DRIFT';
 END IF;
 EXECUTE replace(definition,'tables_to_purge constant text[]:=ARRAY[',
  'tables_to_purge constant text[]:=ARRAY[''v30_import_person_mask_confirmations'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
