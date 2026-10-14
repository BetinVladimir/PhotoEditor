-- +goose Up
-- Identity outputs need independent report/mask/pixel verification before
-- publishing an adoption event. The generic inventory gate has no access to
-- document bytes and must not make an identity result authoritative.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_pre_v23_identity;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_v23_identity(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
  AND operation='identity.compare') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_pre_v23_identity(p_tenant,p_task,p_attempt,
  p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
