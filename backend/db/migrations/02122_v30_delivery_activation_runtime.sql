-- +goose Up
-- Check the active bucket again at the atomic queued transition. A draft may
-- have taken longer than the scheduler lease; completed ticket replay remains
-- ahead of this gate in the copied function.
-- +goose StatementBegin
DO $$
DECLARE definition text; old_name text; old_signature text; old_seal text;
BEGIN
 definition:=pg_get_functiondef('photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text)'::regprocedure);
 old_name:='activate_v30_delivery_create(';
 old_signature:='p_request_sha text)';
 old_seal:=' IF photo.seal_v2_graph(p_tenant,rid) IS DISTINCT FROM true THEN';
 IF (length(definition)-length(replace(definition,old_name,'')))/length(old_name)<>1
  OR (length(definition)-length(replace(definition,old_signature,'')))/length(old_signature)<>1
  OR (length(definition)-length(replace(definition,old_seal,'')))/length(old_seal)<>1 THEN
  RAISE EXCEPTION 'Unexpected V30 delivery activation definition';
 END IF;
 definition:=replace(definition,old_name,'activate_v30_delivery_create_bound(');
 definition:=replace(definition,old_signature,'p_request_sha text, p_bucket text)');
 definition:=replace(definition,old_seal,$guard$
 IF length(p_bucket) NOT BETWEEN 3 AND 63 OR NOT EXISTS(
  SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
   ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=rid AND d.owner_id=owner
   AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params'
   AND d.state='ready' AND d.bucket=p_bucket AND a.bucket=p_bucket
   AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=d.object_version)
 THEN RETURN NULL; END IF;
 IF photo.v30_delivery_runtime_ready(p_bucket) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_RUNTIME_UNAVAILABLE' USING ERRCODE='P0001';
 END IF;
$guard$||old_seal);
 EXECUTE definition;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text) FROM photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
