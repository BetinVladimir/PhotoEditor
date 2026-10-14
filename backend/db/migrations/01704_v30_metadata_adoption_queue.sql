-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_adoption_committed(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND EXISTS(
 SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.exports x ON x.tenant_id=t.tenant_id AND x.task_id=t.id AND x.status='ready'
 JOIN photo.delivery_items i ON i.tenant_id=x.tenant_id AND i.export_id=x.id AND i.state='ready'
 JOIN photo.artifacts a ON a.tenant_id=x.tenant_id AND a.id=x.artifact_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='metadata.write' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt=p_attempt AND t.fencing_token=p_fence AND z.id=p_result
 AND z.attempt=p_attempt AND z.fencing_token=p_fence AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
 AND a.output_role='encoded_image' AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(t.id,p_result))
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-metadata-verified' AND event_id=photo.v2_adoption_event_id(t.id,p_result)));
$$;
ALTER FUNCTION photo.v30_metadata_adoption_committed(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_adoption_committed(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_adoption_committed(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
CREATE FUNCTION photo.next_v30_metadata_adoption(p_after uuid)
RETURNS TABLE(tenant_id uuid,task_id uuid,attempt integer,fence bigint,result_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;previous text:=current_setting('app.tenant_id',true);
BEGIN
 IF p_after IS NULL THEN RETURN;END IF;
 FOR candidate IN SELECT t.tenant_id,t.id,t.attempt,t.fencing_token,t.canonical_result_id FROM photo.tasks t
 WHERE t.id>p_after AND t.operation='metadata.write' AND t.operation_version='1' AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.canonical_result_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=t.tenant_id AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.id LOOP
  PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
  IF photo.v30_metadata_result_ready(candidate.tenant_id,candidate.id,candidate.attempt,candidate.fencing_token,candidate.canonical_result_id) IS TRUE THEN
   PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
   RETURN QUERY SELECT candidate.tenant_id,candidate.id,candidate.attempt,candidate.fencing_token,candidate.canonical_result_id;RETURN;
  END IF;
 END LOOP;
 PERFORM set_config('app.tenant_id',coalesce(previous,''),true);
END $$;
ALTER FUNCTION photo.next_v30_metadata_adoption(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v30_metadata_adoption(uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.next_v30_metadata_adoption(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
