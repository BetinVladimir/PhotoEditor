-- +goose Up
-- Original consumer context only; does not authorize replacement publication.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_negative_replacement_snapshot(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_original uuid,p_snapshot uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;again jsonb;
BEGIN
 IF p_snapshot IS NULL OR p_snapshot='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 intent:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_original);
 IF intent IS NULL THEN RETURN false;END IF;
 PERFORM r.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_original AND t.run_id=(intent->>'original_run_id')::uuid
 AND t.operation=intent->>'operation' AND t.operation_version='1'
 AND t.state IN('succeeded','failed','cancelled','skipped')
 AND r.kind='v2' AND r.input_snapshot_id=p_snapshot
 AND w.project_id=p_project AND w.input_snapshot_id=p_snapshot
 FOR SHARE OF t,r,w;
 IF NOT FOUND THEN RETURN false;END IF;
 again:=photo.read_v29_negative_frozen_intent(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_original);
 RETURN again IS NOT NULL AND again=intent;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_negative_replacement_snapshot(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_replacement_snapshot(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_negative_replacement_snapshot(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
