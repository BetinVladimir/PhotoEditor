-- +goose Up
-- Private binding of execution profile to exact imported qualification and registry.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_downstream_profile_ready(p_tenant uuid,p_project uuid,p_operation text,p_version text,p_class text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; field text; backend text;
BEGIN
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_class IS NULL OR p_operation IS NULL OR p_version IS NULL THEN RETURN false;END IF;
 pin:=p_profile->'qualification';backend:=p_profile->>'backend';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR backend IS NULL OR backend NOT IN('cpu','cuda','mps') THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'] LOOP
 IF jsonb_typeof(p_profile->field) IS DISTINCT FROM 'string' OR coalesce(p_profile->>field,'') !~ '^[0-9a-f]{64}$'
 OR p_profile->field IS DISTINCT FROM pin->field THEN RETURN false;END IF;
 END LOOP;
 IF pin->>'backend' IS DISTINCT FROM backend THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation=p_operation AND version=p_version
 AND resource_class=p_class AND backends ? backend FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.v27_downstream_qualification_pin_ready(p_tenant,p_project,p_operation,p_version,backend,pin);
END $$;
ALTER FUNCTION photo.v27_downstream_profile_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_downstream_profile_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
