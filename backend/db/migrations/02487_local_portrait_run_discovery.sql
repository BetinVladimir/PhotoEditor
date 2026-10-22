-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.discover_v28_local_portrait_result(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(run_id uuid,workflow_id uuid)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='v2' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND photo.v2_workflows.run_id=r.id FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 run_id:=r.id;workflow_id:=NULL;
 IF r.status='completed' AND NOT r.cancel_requested AND r.v28_local_result_receipt IS NOT NULL
 AND r.v28_local_result_receipt->'review_id'='null'::jsonb
 AND EXISTS(SELECT FROM photo.get_v28_local_portrait_result(p_tenant,p_actor,p_session,w.id) selected
   WHERE selected.run_id=r.id AND selected.operation IN('portrait.hair','portrait.clothing'))
 THEN workflow_id:=w.id;END IF;
 RETURN NEXT;
END $$;
ALTER FUNCTION photo.discover_v28_local_portrait_result(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.discover_v28_local_portrait_result(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.discover_v28_local_portrait_result(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
