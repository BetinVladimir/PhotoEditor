-- +goose Up
-- Additional fenced entry points; legacy API cutover is a separate migration.
-- +goose StatementBegin
CREATE FUNCTION photo.grant_v30_import_person_mask_cas(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_parsed text,p_recipe_id uuid,p_recipe_raw text,p_override_id uuid,p_override_raw text,p_mask_raw text,p_target_version uuid,p_expected_epoch bigint)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE old photo.v30_import_person_mask_confirmations%ROWTYPE;v_mask uuid;result bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_expected_epoch IS NULL OR p_expected_epoch<0 OR p_expected_epoch>=9007199254740991
 THEN RETURN NULL;END IF;
 -- Serialize absence as well as an existing confirmation with the legacy writer.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 v_mask:=(p_mask_raw::jsonb#>>'{artifact,artifact_id}')::uuid;
 IF v_mask IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO old FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow AND c.entry_id=p_entry
 AND c.override_id=p_override_id AND c.mask_id=v_mask AND c.target_version_id=p_target_version FOR UPDATE;
 IF FOUND THEN
  IF NOT(old.epoch=p_expected_epoch AND old.status<>'granted'
   OR old.epoch=p_expected_epoch+1 AND old.status='granted') THEN RETURN NULL;END IF;
 ELSE
  IF p_expected_epoch<>0 OR 'grant'<>'grant' THEN RETURN NULL;END IF;
 END IF;
 result:=photo.grant_v30_import_person_mask(p_tenant,p_actor,p_session,p_workflow,p_entry,p_parsed,p_recipe_id,p_recipe_raw,p_override_id,p_override_raw,p_mask_raw,p_target_version);
 IF result IS NOT NULL AND result<>p_expected_epoch+1 THEN
  RAISE EXCEPTION 'mask decision epoch drift' USING ERRCODE='23514';
 END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.grant_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.grant_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid,bigint) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.grant_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid,bigint) TO photo_api;
CREATE FUNCTION photo.revoke_v30_import_person_mask_cas(
 p_tenant uuid,p_actor uuid,p_session uuid,p_workflow uuid,p_entry uuid,
 p_override_id uuid,p_mask_id uuid,p_target_version uuid,p_expected_epoch bigint)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE old photo.v30_import_person_mask_confirmations%ROWTYPE;v_mask uuid;result bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_expected_epoch IS NULL OR p_expected_epoch<0 OR p_expected_epoch>=9007199254740991
 THEN RETURN NULL;END IF;
 -- Serialize absence as well as an existing confirmation with the legacy writer.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 v_mask:=p_mask_id;
 IF v_mask IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO old FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.workflow_id=p_workflow AND c.entry_id=p_entry
 AND c.override_id=p_override_id AND c.mask_id=v_mask AND c.target_version_id=p_target_version FOR UPDATE;
 IF FOUND THEN
  IF NOT(old.epoch=p_expected_epoch AND old.status<>'revoked'
   OR old.epoch=p_expected_epoch+1 AND old.status='revoked') THEN RETURN NULL;END IF;
 ELSE
  IF p_expected_epoch<>0 OR 'revoke'<>'grant' THEN RETURN NULL;END IF;
 END IF;
 result:=photo.revoke_v30_import_person_mask(p_tenant,p_actor,p_session,p_workflow,p_entry,p_override_id,p_mask_id,p_target_version);
 IF result IS NOT NULL AND result<>p_expected_epoch+1 THEN
  RAISE EXCEPTION 'mask decision epoch drift' USING ERRCODE='23514';
 END IF;
 RETURN result;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.revoke_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.revoke_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.revoke_v30_import_person_mask_cas(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
