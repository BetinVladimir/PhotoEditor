-- +goose Up
-- Atomic caller wrapper: no partial decision/version survives a retention refusal.
-- +goose StatementBegin
CREATE FUNCTION photo.apply_v29_multi_retained(p_tenant uuid,p_project uuid,p_operation text,p_task uuid,
 p_actor uuid,p_session uuid,p_decision jsonb,p_root text,p_image text,p_witnesses jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity uuid;r photo.runs%ROWTYPE;w jsonb;v_ordinal integer:=0;v_purpose text;old photo.v2_artifact_links%ROWTYPE;
BEGIN
 identity:=photo.apply_v29_multi_version(p_tenant,p_project,p_operation,p_task,p_actor,p_session,p_decision,p_image);
 IF identity IS NULL THEN RETURN NULL;END IF;
 IF photo.v29_multi_applied_witnesses_current(p_tenant,(p_decision->>'quality_run_id')::uuid,p_root,p_image,p_witnesses) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'MULTI_APPLY_RETENTION_STALE' USING ERRCODE='40001';
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(p_decision->>'quality_run_id')::uuid;
 v_purpose:='multi.applied:'||identity::text;
 FOR w IN SELECT value FROM jsonb_array_elements(p_witnesses) LOOP
  SELECT * INTO old FROM photo.v2_artifact_links WHERE tenant_id=p_tenant
   AND workflow_id=(r.config_snapshot->>'source_workflow_id')::uuid AND v2_artifact_links.purpose=v_purpose AND v2_artifact_links.ordinal=v_ordinal;
  IF FOUND THEN
   IF old.project_id IS DISTINCT FROM p_project OR old.artifact_id IS DISTINCT FROM (w->'ref'->>'artifact_id')::uuid OR old.sha256 IS DISTINCT FROM w->'ref'->>'sha256' THEN
    RAISE EXCEPTION 'MULTI_APPLY_RETENTION_CONFLICT' USING ERRCODE='40001';
   END IF;
  ELSE
   INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
   VALUES(p_tenant,p_project,(r.config_snapshot->>'source_workflow_id')::uuid,(w->'ref'->>'artifact_id')::uuid,w->'ref'->>'sha256',v_purpose,v_ordinal);
  END IF;
  v_ordinal:=v_ordinal+1;
 END LOOP;
 RETURN identity;
END $$;
ALTER FUNCTION photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.apply_v29_multi_retained(uuid,uuid,text,uuid,uuid,uuid,jsonb,text,text,jsonb) TO photo_api;
-- Force callers through the complete retention transaction.
REVOKE EXECUTE ON FUNCTION photo.apply_v29_multi_version(uuid,uuid,text,uuid,uuid,uuid,jsonb,text) FROM photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
