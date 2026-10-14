-- +goose Up
-- Trusted coordinator role only. Never grant membership to API/worker accounts.
-- Object-byte verification remains the trusted Go coordinator's responsibility.
-- +goose StatementBegin
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='photo_semantic_admission') THEN
 CREATE ROLE photo_semantic_admission NOLOGIN NOINHERIT NOBYPASSRLS;
 END IF;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='photo_semantic_admission' AND
 (rolsuper OR rolcanlogin OR rolinherit OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication))
 OR EXISTS(SELECT 1 FROM pg_auth_members WHERE member='photo_semantic_admission'::regrole) THEN
 RAISE EXCEPTION 'SEMANTIC_ADMISSION_ROLE_UNSAFE';END IF;
END $$;
GRANT USAGE ON SCHEMA photo TO photo_semantic_admission;
GRANT SELECT ON photo.idempotency_records TO photo_semantic_admission;
GRANT INSERT(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at),
 UPDATE(response_status,response_body,resource_id) ON photo.idempotency_records TO photo_semantic_admission;
CREATE POLICY semantic_admission_receipt ON photo.idempotency_records TO photo_semantic_admission
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND principal_type='user' AND route='SemanticIntent.Apply')
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid AND principal_type='user' AND route='SemanticIntent.Apply');

CREATE FUNCTION photo.lock_semantic_apply_resources(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session)) THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.lock_semantic_apply_resources(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_apply_resources(uuid,uuid,uuid,uuid) FROM PUBLIC;

-- Resolve only this migration's explicit function allowlist. Require one exact
-- existing overload, trusted owner and SECURITY DEFINER before granting.
DO $$ DECLARE name text; proc regprocedure; count integer; BEGIN
 FOREACH name IN ARRAY ARRAY[
 'semantic_review_identity','semantic_approved_protection_pin',
 'semantic_approved_protection_object','semantic_approved_protection_origin',
 'semantic_approved_protection_binary','semantic_approved_image_object',
 'semantic_approved_image_binary','semantic_approved_candidate_child',
 'semantic_approved_mask_binary','create_semantic_apply_draft_shell',
 'register_semantic_apply_inputs','seal_semantic_apply_inputs',
 'finish_semantic_apply_review','lock_semantic_apply_resources'] LOOP
 SELECT count(*),min(p.oid::bigint)::oid::regprocedure INTO count,proc FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='photo' AND p.proname=name AND p.prosecdef AND p.proowner='photo_fn_owner'::regrole;
 IF count<>1 THEN RAISE EXCEPTION 'SEMANTIC_ADMISSION_FUNCTION_UNAVAILABLE';END IF;
 EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO photo_semantic_admission',proc);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
