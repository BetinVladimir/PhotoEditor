-- +goose Up
-- Successful semantic runs require their deterministic CPU assembly node.
-- This guard also covers the interval before an expansion barrier is staged.
-- +goose StatementBegin
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) RENAME TO finalize_v2_run_pre_semantic;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_semantic(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; h bytea; expected uuid;
BEGIN
 -- Preserve the finalizer/adopter lock order and serialize graph publication.
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.kind='v2' AND r.status IN ('queued','running') AND NOT r.cancel_requested
 AND w.workflow_type='semantic'
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state IN ('failed','cancelled','skipped')) THEN
  h:=public.digest(uuid_send(w.id)||convert_to('v2.semantic.assemble:0','UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
   AND t.id=expected AND t.node_key='v2.semantic.assemble:0'
   AND t.operation='v2.semantic.assemble' AND t.operation_version='1' AND t.resource_class='cpu')
  THEN RETURN 'pending';END IF;
 END IF;
 RETURN photo.finalize_v2_run_pre_semantic(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
