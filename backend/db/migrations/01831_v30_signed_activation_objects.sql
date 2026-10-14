-- +goose Up
-- Scheduler-only exact-version object inventory for pre-activation physical
-- V31 verification. Caller must stream all rows, renew lease, then read again
-- and compare before invoking the atomic signed activation transition.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_activation_objects(
 p_expansion uuid,p_token uuid,p_task uuid,p_params uuid,p_sha text,p_json text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;source jsonb;previous text;
BEGIN
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR SHARE;
 IF NOT FOUND OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
  OR e.expansion_lease_until<=clock_timestamp() OR e.cursor::integer<>e.expected_count
  OR p_task IS DISTINCT FROM photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),
   'v2.delivery.assemble:0') THEN RETURN;END IF;
 previous:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',e.tenant_id::text,true);
 BEGIN
  IF photo.v30_delivery_signed_current_matrix(e.tenant_id,e.run_id,p_task,
   p_params,p_sha,p_json) IS DISTINCT FROM true THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RETURN;END IF;
  source:=photo.v30_delivery_signed_runtime_documents(e.tenant_id,e.run_id,
   p_task,p_params,p_sha,p_json);
  IF source IS NULL THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RETURN;END IF;
  RETURN QUERY
   SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
   FROM jsonb_array_elements((source->>'closure_jcs')::jsonb->'entries')
    WITH ORDINALITY entry(value,position)
   CROSS JOIN LATERAL (VALUES
    (entry.value->'signed',1),(entry.value->'manifest',2)) ref(body,kind)
   JOIN photo.artifacts a ON a.tenant_id=e.tenant_id
    AND a.id=(ref.body->>'artifact_id')::uuid
    AND a.sha256=ref.body->>'sha256' AND a.state='ready'
    AND a.verified_at IS NOT NULL AND a.created_by_task_id=(entry.value->>'task_id')::uuid
    AND a.output_role=CASE ref.kind WHEN 1 THEN 'signed_export' ELSE 'manifest' END
    AND a.media_type=CASE ref.kind WHEN 1 THEN
     (SELECT original.media_type FROM photo.artifacts original
      WHERE original.tenant_id=e.tenant_id
       AND original.id=(entry.value->'export'->>'artifact_id')::uuid)
     ELSE 'application/c2pa' END
    AND a.bytes BETWEEN 1 AND CASE ref.kind WHEN 1 THEN 536870912 ELSE 8388608 END
    AND a.object_version NOT IN('','null')
   ORDER BY entry.position,ref.kind;
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous,''),true);RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signed_activation_objects(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
