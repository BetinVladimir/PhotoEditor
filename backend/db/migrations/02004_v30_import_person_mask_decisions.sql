-- +goose Up
-- A user decision never comes from the archive's confirmed flag. Verify the
-- exact immutable chain before recording a grant; revoke remains available
-- after the import run has left its preparation state.
-- +goose StatementBegin
CREATE FUNCTION photo.grant_v30_import_person_mask(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_parsed text,p_recipe_id uuid,p_recipe_raw text,p_override_id uuid,
 p_override_raw text,p_mask_raw text,p_target_version uuid)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;r photo.runs%ROWTYPE;
 sources jsonb;parsed jsonb;recipe jsonb;override_body jsonb;mask_body jsonb;
 entry_body jsonb;recipe_file jsonb;override_item jsonb;override_file jsonb;
 pixel_item jsonb;pixel_file jsonb;match_count integer;
 recipe_sha text;override_sha text;mask_id uuid;mask_sha text;source_version uuid;
 old photo.v30_import_person_mask_confirmations%ROWTYPE;next_epoch bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_parsed IS NULL OR octet_length(p_parsed) NOT BETWEEN 2 AND 8388608
 OR p_recipe_raw IS NULL OR octet_length(p_recipe_raw) NOT BETWEEN 2 AND 1048576
 OR p_override_raw IS NULL OR octet_length(p_override_raw) NOT BETWEEN 2 AND 8388608
 OR p_mask_raw IS NULL OR octet_length(p_mask_raw) NOT BETWEEN 2 AND 1048576
 OR p_actor IS NULL OR p_session IS NULL OR p_entry IS NULL
 OR p_recipe_id IS NULL OR p_override_id IS NULL OR p_target_version IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR SHARE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested THEN RETURN NULL;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_parsed);
 IF sources IS NULL OR sources->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,entry_body
 FROM jsonb_array_elements(p_parsed::jsonb->'manifest'->'entries')
 WHERE value->>'entry_id'=p_entry::text;
 IF match_count<>1 OR entry_body->>'recipe_path' IS NULL THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(sources->'entries') e
  WHERE e->>'entry_id'=p_entry::text AND e->>'state'='adopted'
  AND e->>'asset_version_id'=p_target_version::text) THEN RETURN NULL;END IF;
 parsed:=p_parsed::jsonb;recipe:=p_recipe_raw::jsonb;
 override_body:=p_override_raw::jsonb;mask_body:=p_mask_raw::jsonb;
 recipe_sha:=encode(sha256(convert_to(p_recipe_raw,'UTF8')),'hex');
 override_sha:=encode(sha256(convert_to(p_override_raw,'UTF8')),'hex');
 IF entry_body->>'recipe_sha256' IS DISTINCT FROM recipe_sha
 OR recipe->>'schema_version' IS DISTINCT FROM '3'
 OR recipe->'source'->>'asset_version_id' IS NULL THEN RETURN NULL;END IF;
 source_version:=(recipe->'source'->>'asset_version_id')::uuid;
 IF source_version='00000000-0000-0000-0000-000000000000'::uuid
 OR source_version=p_target_version THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,recipe_file FROM jsonb_array_elements(parsed->'files')
 WHERE value->>'path'=entry_body->>'recipe_path';
 IF match_count<>1 OR recipe_file->>'contract' IS DISTINCT FROM 'EditingRecipeV3'
 OR recipe_file#>>'{content,artifact_id}' IS DISTINCT FROM p_recipe_id::text
 OR recipe_file#>>'{content,sha256}' IS DISTINCT FROM recipe_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=p_recipe_id AND a.sha256=recipe_sha AND a.state='ready'
  AND a.media_type='application/octet-stream' AND a.output_role='imported_file'
  AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 SELECT count(*) INTO match_count FROM jsonb_array_elements(recipe->'overrides') ref
 WHERE ref->>'artifact_id'=p_override_id::text AND ref->>'sha256'=override_sha;
 IF match_count<>1 THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,override_item
 FROM jsonb_array_elements(parsed->'manifest'->'inventory')
 WHERE value->>'artifact_id'=p_override_id::text AND value->>'sha256'=override_sha
 AND value->>'contract'='Override';
 IF match_count<>1 THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,override_file FROM jsonb_array_elements(parsed->'files')
 WHERE value->>'path'=override_item->>'path';
 IF match_count<>1 OR override_file->>'contract' IS DISTINCT FROM 'Override'
 OR override_file#>>'{content,sha256}' IS DISTINCT FROM override_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(override_file#>>'{content,artifact_id}')::uuid AND a.sha256=override_sha
  AND a.state='ready' AND a.media_type='application/octet-stream'
  AND a.output_role='imported_file' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 IF mask_body->>'selector' IS DISTINCT FROM 'person'
 OR mask_body->'object_id' IS DISTINCT FROM 'null'::jsonb
 OR coalesce(mask_body->>'model_lock_id','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 mask_id:=(mask_body#>>'{artifact,artifact_id}')::uuid;mask_sha:=mask_body#>>'{artifact,sha256}';
 IF mask_id IS NULL OR coalesce(mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT count(*) INTO match_count FROM jsonb_array_elements(override_body->'mask_refs') ref
 WHERE ref->'mask'=mask_body;
 IF match_count<>1 THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,pixel_item
 FROM jsonb_array_elements(parsed->'manifest'->'inventory')
 WHERE value->>'artifact_id'=mask_id::text AND value->>'sha256'=mask_sha
 AND value->>'contract'='MaskPixels';
 IF match_count<>1 THEN RETURN NULL;END IF;
 SELECT count(*),jsonb_agg(value)->0 INTO match_count,pixel_file FROM jsonb_array_elements(parsed->'files')
 WHERE value->>'path'=pixel_item->>'path';
 IF match_count<>1 OR pixel_file->>'contract' IS DISTINCT FROM 'MaskPixels'
 OR pixel_file#>>'{content,sha256}' IS DISTINCT FROM mask_sha THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=(pixel_file#>>'{content,artifact_id}')::uuid AND a.sha256=mask_sha
  AND a.state='ready' AND a.media_type='application/octet-stream'
  AND a.output_role='imported_file' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 SELECT * INTO old FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow AND c.entry_id=p_entry
 AND c.override_id=p_override_id AND c.mask_id=mask_id AND c.target_version_id=p_target_version FOR UPDATE;
 IF FOUND THEN
  IF old.project_id<>w.project_id OR old.recipe_id<>p_recipe_id OR old.recipe_sha<>recipe_sha
  OR old.override_sha<>override_sha OR old.mask_sha<>mask_sha
  OR old.source_version_id<>source_version THEN RETURN NULL;END IF;
  IF old.status='granted' THEN RETURN old.epoch;END IF;
  UPDATE photo.v30_import_person_mask_confirmations SET status='granted',epoch=epoch+1,
   actor_id=p_actor,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND workflow_id=p_workflow AND entry_id=p_entry
  AND override_id=p_override_id AND mask_id=mask_id AND target_version_id=p_target_version
  RETURNING epoch INTO next_epoch;
  RETURN next_epoch;
 END IF;
 INSERT INTO photo.v30_import_person_mask_confirmations(
  tenant_id,workflow_id,project_id,entry_id,recipe_id,recipe_sha,
  override_id,override_sha,mask_id,mask_sha,source_version_id,target_version_id,
  actor_id,status,epoch)
 VALUES(p_tenant,p_workflow,w.project_id,p_entry,p_recipe_id,recipe_sha,
  p_override_id,override_sha,mask_id,mask_sha,source_version,p_target_version,
  p_actor,'granted',1);
 RETURN 1;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value
 OR unique_violation THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.grant_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.grant_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.grant_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid)
 TO photo_api;

CREATE FUNCTION photo.revoke_v30_import_person_mask(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_override uuid,p_mask uuid,p_target_version uuid)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;old photo.v30_import_person_mask_confirmations%ROWTYPE;next_epoch bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
 AND workflow_type='interchange' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO old FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow AND c.project_id=w.project_id
 AND c.entry_id=p_entry AND c.override_id=p_override AND c.mask_id=p_mask
 AND c.target_version_id=p_target_version FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF old.status='revoked' THEN RETURN old.epoch;END IF;
 UPDATE photo.v30_import_person_mask_confirmations SET status='revoked',epoch=epoch+1,
  actor_id=p_actor,updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND workflow_id=p_workflow AND entry_id=p_entry
 AND override_id=p_override AND mask_id=p_mask AND target_version_id=p_target_version
 RETURNING epoch INTO next_epoch;
 RETURN next_epoch;
END $$;
ALTER FUNCTION photo.revoke_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.revoke_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.revoke_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)
 TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
